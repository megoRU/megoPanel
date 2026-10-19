package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/platform"
)

type BackupRestoreResult struct {
	BackupID           string   `json:"backupId"`
	PreRestoreBackup   string   `json:"preRestoreBackupId"`
	RestoredComponents []string `json:"restoredComponents"`
	RestartScheduled   bool     `json:"restartScheduled"`
}

func (s *BackupService) Restore(ctx context.Context, id string) (*BackupRestoreResult, error) {
	if !backupIDPattern.MatchString(id) {
		err := errors.New("invalid backup id")
		s.recordOperation("restore", err)
		return nil, err
	}
	if !systemOperationMu.TryLock() {
		err := errors.New("another backup or restore operation is already running")
		s.recordOperation("restore", err)
		return nil, err
	}
	defer systemOperationMu.Unlock()
	result, err := s.restoreLocked(ctx, id)
	s.recordOperation("restore", err)
	return result, err
}

func (s *BackupService) restoreLocked(ctx context.Context, id string) (*BackupRestoreResult, error) {
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return nil, err
	}
	archivePath := filepath.Join(directory, id+".zip")
	if err := verifyBackupArchive(archivePath); err != nil {
		return nil, fmt.Errorf("backup verification failed: %w", err)
	}
	manifest, err := readBackupManifest(archivePath)
	if err != nil {
		return nil, err
	}
	if manifest.ID != id || manifest.FormatVersion > backupFormatVersion || !manifestSupportsCurrentVersion(manifest.AppVersion) {
		return nil, errors.New("backup format or application version is not compatible with this MegoPanel version")
	}
	components, err := normalizeBackupComponents(manifest.Components)
	if err != nil {
		return nil, fmt.Errorf("backup manifest has invalid components: %w", err)
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	if err := checkBackupFreeSpace(directory, uint64(archiveInfo.Size())*2+uint64(64<<20)); err != nil {
		return nil, err
	}
	preRestore, err := s.createLocked(ctx, BackupOptions{Components: components}, id)
	if err != nil {
		return nil, fmt.Errorf("could not create a pre-restore backup: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "megopanel-restore-*")
	if err != nil {
		return nil, fmt.Errorf("pre-restore backup %s is available; create restore staging directory: %w", preRestore.ID, err)
	}
	defer os.RemoveAll(tempDir)
	if err := checkBackupFreeSpace(tempDir, backupExpandedSize(archivePath)); err != nil {
		return nil, fmt.Errorf("pre-restore backup %s is available; %w", preRestore.ID, err)
	}
	if err := extractBackupArchive(archivePath, tempDir); err != nil {
		return nil, fmt.Errorf("pre-restore backup %s is available; extract backup: %w", preRestore.ID, err)
	}
	if hasBackupComponent(components, "panel") {
		if err := validateSQLiteDatabase(ctx, filepath.Join(tempDir, "panel.db")); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; panel database failed integrity check: %w", preRestore.ID, err)
		}
		if err := validateRestoredConfig(filepath.Join(tempDir, "config.yaml"), s.databasePath); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; panel config is not compatible: %w", preRestore.ID, err)
		}
	}

	if hasBackupComponent(components, "mariadb") {
		if err := s.restoreMariaDB(ctx, tempDir); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; MariaDB restore failed: %w", preRestore.ID, err)
		}
	}
	if hasBackupComponent(components, "panel") {
		if err := s.restoreSQLiteDatabase(ctx, s.databasePath, filepath.Join(tempDir, "panel.db")); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; panel database restore failed: %w", preRestore.ID, err)
		}
	}
	if hasBackupComponent(components, "websites") {
		if err := restoreWebsiteFiles(filepath.Join(tempDir, "websites"), "/var/www"); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; website files restore failed: %w", preRestore.ID, err)
		}
	}
	if hasBackupComponent(components, "panel") {
		if err := replaceRegularFile(filepath.Join(tempDir, "config.yaml"), s.configPath, 0600); err != nil {
			return nil, fmt.Errorf("pre-restore backup %s is available; panel config restore failed: %w", preRestore.ID, err)
		}
	}

	restarted := false
	if hasBackupComponent(components, "panel") {
		restarted = scheduleBackupRestoreRestart()
	}
	return &BackupRestoreResult{BackupID: id, PreRestoreBackup: preRestore.ID, RestoredComponents: components, RestartScheduled: restarted}, nil
}

func backupExpandedSize(path string) uint64 {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer archive.Close()
	var total uint64
	for _, file := range archive.File {
		if file.UncompressedSize64 > ^uint64(0)-total {
			return ^uint64(0)
		}
		total += file.UncompressedSize64
	}
	return total + uint64(64<<20)
}

func manifestSupportsCurrentVersion(version string) bool {
	if _, _, ok := parseVersion(version); !ok {
		return false
	}
	return !isNewerVersion(CurrentVersion, version)
}

func extractBackupArchive(archivePath, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	var total uint64
	for _, file := range archive.File {
		if file.Name == "manifest.json" {
			continue
		}
		mode := file.FileInfo().Mode()
		if !validBackupEntryName(file.Name) || mode&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !mode.IsRegular()) {
			return errors.New("backup contains an unsafe file path or file type")
		}
		if file.UncompressedSize64 > maxBackupEntrySize || file.UncompressedSize64 > maxBackupEntrySize-total {
			return errors.New("backup expands beyond the allowed size")
		}
		total += file.UncompressedSize64
		clean := filepath.Clean(filepath.FromSlash(file.Name))
		target := filepath.Join(destination, clean)
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
			return errors.New("backup path escapes restore staging directory")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			output.Close()
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, int64(file.UncompressedSize64)+1))
		closeInputErr := input.Close()
		syncErr := output.Sync()
		closeOutputErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInputErr != nil {
			return closeInputErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeOutputErr != nil {
			return closeOutputErr
		}
		if uint64(written) != file.UncompressedSize64 {
			return fmt.Errorf("backup entry %s had an unexpected size", file.Name)
		}
	}
	return nil
}

func validateSQLiteDatabase(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	sqlitePath, err := exec.LookPath("sqlite3")
	if err != nil {
		return errors.New("sqlite3 utility is required to validate a panel database backup")
	}
	output, err := exec.CommandContext(ctx, sqlitePath, path, "PRAGMA integrity_check;").CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite integrity check failed: %w", err)
	}
	if strings.TrimSpace(string(output)) != "ok" {
		return errors.New("sqlite integrity check did not return ok")
	}
	return nil
}

func validateRestoredConfig(configPath, currentDatabasePath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var restored config.Config
	if err := yaml.Unmarshal(data, &restored); err != nil {
		return err
	}
	if restored.Database.Path == "" {
		return errors.New("database path is missing")
	}
	restoredPath, err := filepath.Abs(restored.Database.Path)
	if err != nil {
		return err
	}
	currentPath, err := filepath.Abs(currentDatabasePath)
	if err != nil {
		return err
	}
	if filepath.Clean(restoredPath) != filepath.Clean(currentPath) {
		return errors.New("backup database path differs from the active panel database path")
	}
	if len(restored.Security.JWTSecret) < 32 || len(restored.Security.CSRFSecret) < 32 {
		return errors.New("backup config does not contain valid security secrets")
	}
	return nil
}

func (s *BackupService) restoreSQLiteDatabase(ctx context.Context, destination, source string) error {
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("panel database path must be a regular file")
	}
	if err := validateSQLiteDatabase(ctx, source); err != nil {
		return err
	}
	if err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		return err
	}
	sqlitePath, err := exec.LookPath("sqlite3")
	if err != nil {
		return errors.New("sqlite3 utility is required to restore a panel database")
	}
	command := ".restore \"" + strings.ReplaceAll(strings.ReplaceAll(source, `\`, `\\`), `"`, `\"`) + `"`
	output, err := exec.CommandContext(ctx, sqlitePath, destination, command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite restore failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return validateSQLiteDatabase(ctx, destination)
}

func (s *BackupService) restoreMariaDB(ctx context.Context, stagingDir string) error {
	password, exists, err := s.settings.Get("mariadb_admin_password")
	if err != nil || !exists || password == "" {
		return errors.New("MegoPanel MariaDB credentials are not configured")
	}
	dump, err := os.Open(filepath.Join(stagingDir, "mariadb.sql"))
	if err != nil {
		return err
	}
	if err := platform.MariaDBRestore(ctx, "megopanel", password, dump); err != nil {
		dump.Close()
		return err
	}
	if err := dump.Close(); err != nil {
		return err
	}
	return restoreMariaDBUsers(ctx, password, filepath.Join(stagingDir, "database-users.json"))
}

func restoreMariaDBUsers(ctx context.Context, adminPassword, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var users []backupDatabaseUser
	if err := json.Unmarshal(data, &users); err != nil {
		return err
	}
	output, err := platform.MariaDBQuery(ctx, "megopanel", adminPassword, "SHOW DATABASES;")
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		existing[strings.TrimSpace(line)] = true
	}
	for _, user := range users {
		if !platformValidDatabaseName(user.Name) || isBackupSystemDatabase(user.Name) || user.Password == "" || len(user.Password) > 256 || !existing[user.Name] {
			continue
		}
		literal, err := platform.MariaDBStringLiteral(user.Password)
		if err != nil {
			return err
		}
		account := "'" + user.Name + "'@'localhost'"
		query := "CREATE USER IF NOT EXISTS " + account + " IDENTIFIED VIA mysql_native_password USING PASSWORD(" + literal + "); " +
			"ALTER USER " + account + " IDENTIFIED VIA mysql_native_password USING PASSWORD(" + literal + "); " +
			"GRANT ALL PRIVILEGES ON `" + user.Name + "`.* TO " + account + "; FLUSH PRIVILEGES;"
		if _, err := platform.MariaDBQuery(ctx, "megopanel", adminPassword, query); err != nil {
			return fmt.Errorf("restore database account %s: %w", user.Name, err)
		}
	}
	return nil
}

func restoreWebsiteFiles(stagingRoot, destinationRoot string) error {
	rootInfo, err := os.Lstat(destinationRoot)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("website data root must be a real directory")
	}
	return filepath.WalkDir(stagingRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(stagingRoot, path)
		if err != nil || relative == "." {
			return err
		}
		if relative == "phpmyadmin" || strings.HasPrefix(relative, "phpmyadmin"+string(os.PathSeparator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return errors.New("backup must not replace phpMyAdmin application files")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("backup contains a website symlink")
		}
		target := filepath.Join(destinationRoot, relative)
		if err := rejectSymlinkPath(filepath.Dir(target)); err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			return os.Chmod(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if info, err := os.Lstat(target); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("website restore target is not a regular file")
		}
		temporary, err := os.CreateTemp(filepath.Dir(target), ".megopanel-restore-*")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0644); err != nil {
			temporary.Close()
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			temporary.Close()
			return err
		}
		_, copyErr := io.Copy(temporary, input)
		input.Close()
		if copyErr != nil {
			temporary.Close()
			return copyErr
		}
		if err := temporary.Sync(); err != nil {
			temporary.Close()
			return err
		}
		if err := temporary.Close(); err != nil {
			return err
		}
		return os.Rename(temporaryPath, target)
	})
}

func replaceRegularFile(source, destination string, mode os.FileMode) error {
	if err := rejectSymlinkPath(filepath.Dir(destination)); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("restore destination is not a regular file")
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".megopanel-restore-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	input, err := os.Open(source)
	if err != nil {
		temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	input.Close()
	if copyErr != nil {
		temporary.Close()
		return copyErr
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	return nil
}

func scheduleBackupRestoreRestart() bool {
	unit := "megopanel-restore-" + strconv.FormatInt(time.Now().Unix(), 10)
	return exec.CommandContext(context.Background(), "systemd-run", "--quiet", "--unit="+unit, "--on-active=3s", "/bin/systemctl", "restart", "megopanel").Run() == nil
}
