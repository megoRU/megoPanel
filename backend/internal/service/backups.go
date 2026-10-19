package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/repository"
)

const (
	backupFormatVersion = 1
	defaultBackupDir    = "/var/lib/megopanel/backups"
	maxBackupEntrySize  = uint64(20 << 30)
)

var backupIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

type BackupManifest struct {
	FormatVersion int       `json:"formatVersion"`
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"createdAt"`
	AppVersion    string    `json:"appVersion"`
	Components    []string  `json:"components"`
}

type BackupRecord struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	AppVersion string    `json:"appVersion"`
	Components []string  `json:"components"`
	SizeBytes  int64     `json:"sizeBytes"`
	Verified   bool      `json:"verified"`
}

type BackupSettings struct {
	Directory          string   `json:"directory"`
	ScheduleEnabled    bool     `json:"scheduleEnabled"`
	ScheduleHour       int      `json:"scheduleHour"`
	RetentionCount     int      `json:"retentionCount"`
	RetentionDays      int      `json:"retentionDays"`
	ScheduleComponents []string `json:"scheduleComponents"`
}

type BackupOptions struct {
	Components []string `json:"components"`
}

type BackupOperationEvent struct {
	At      time.Time `json:"at"`
	Action  string    `json:"action"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

type BackupService struct {
	db           *gorm.DB
	settings     repository.SettingRepository
	databasePath string
	configPath   string
}

func NewBackupService(db *gorm.DB, settings repository.SettingRepository, databasePath, configPath string) *BackupService {
	return &BackupService{db: db, settings: settings, databasePath: databasePath, configPath: configPath}
}

func (s *BackupService) Settings() (BackupSettings, error) {
	settings := BackupSettings{Directory: defaultBackupDir, ScheduleHour: 3, RetentionCount: 10, RetentionDays: 30, ScheduleComponents: []string{"panel", "mariadb", "websites"}}
	values := []struct {
		key string
		set func(string) error
	}{
		{"backups_directory", func(v string) error { settings.Directory = v; return nil }},
		{"backups_schedule_enabled", func(v string) error { settings.ScheduleEnabled = v == "true"; return nil }},
		{"backups_schedule_hour", func(v string) error { n, err := strconv.Atoi(v); settings.ScheduleHour = n; return err }},
		{"backups_retention_count", func(v string) error { n, err := strconv.Atoi(v); settings.RetentionCount = n; return err }},
		{"backups_retention_days", func(v string) error { n, err := strconv.Atoi(v); settings.RetentionDays = n; return err }},
	}
	for _, field := range values {
		value, exists, err := s.settings.Get(field.key)
		if err != nil {
			return settings, fmt.Errorf("read backup setting %s: %w", field.key, err)
		}
		if exists {
			if err := field.set(value); err != nil {
				return settings, fmt.Errorf("invalid backup setting %s", field.key)
			}
		}
	}
	components, exists, err := s.settings.Get("backups_schedule_components")
	if err != nil {
		return settings, fmt.Errorf("read scheduled backup components: %w", err)
	}
	if exists {
		if err := json.Unmarshal([]byte(components), &settings.ScheduleComponents); err != nil {
			return settings, errors.New("scheduled backup components are invalid")
		}
	}
	if err := validateBackupSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (s *BackupService) UpdateSettings(next BackupSettings) (BackupSettings, error) {
	if err := validateBackupSettings(next); err != nil {
		return BackupSettings{}, err
	}
	components, err := json.Marshal(next.ScheduleComponents)
	if err != nil {
		return BackupSettings{}, err
	}
	for key, value := range map[string]string{
		"backups_directory":       filepath.Clean(next.Directory),
		"backups_schedule_enabled": strconv.FormatBool(next.ScheduleEnabled),
		"backups_schedule_hour":    strconv.Itoa(next.ScheduleHour),
		"backups_retention_count":  strconv.Itoa(next.RetentionCount),
		"backups_retention_days":   strconv.Itoa(next.RetentionDays),
		"backups_schedule_components": string(components),
	} {
		if err := s.settings.Set(key, value); err != nil {
			return BackupSettings{}, fmt.Errorf("save backup setting %s: %w", key, err)
		}
	}
	return s.Settings()
}

func validateBackupSettings(settings BackupSettings) error {
	if !allowedBackupDirectory(settings.Directory) {
		return errors.New("backup directory must be inside /var/lib/megopanel or /var/backups/megopanel")
	}
	if settings.ScheduleHour < 0 || settings.ScheduleHour > 23 {
		return errors.New("backup schedule hour must be between 0 and 23")
	}
	if settings.RetentionCount < 1 || settings.RetentionCount > 1000 {
		return errors.New("backup retention count must be between 1 and 1000")
	}
	if settings.RetentionDays < 1 || settings.RetentionDays > 3650 {
		return errors.New("backup retention days must be between 1 and 3650")
	}
	components, err := normalizeBackupComponents(settings.ScheduleComponents)
	if err != nil || len(components) != len(settings.ScheduleComponents) {
		return errors.New("select at least one valid component for scheduled backups")
	}
	return nil
}

func allowedBackupDirectory(directory string) bool {
	if !filepath.IsAbs(directory) {
		return false
	}
	clean := filepath.Clean(directory)
	for _, root := range []string{defaultBackupDir, "/var/backups/megopanel"} {
		relative, err := filepath.Rel(root, clean)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (s *BackupService) Create(ctx context.Context, options BackupOptions) (*BackupRecord, error) {
	if !systemOperationMu.TryLock() {
		err := errors.New("another backup or restore operation is already running")
		s.recordOperation("create", err)
		return nil, err
	}
	defer systemOperationMu.Unlock()
	backup, err := s.createLocked(ctx, options)
	s.recordOperation("create", err)
	return backup, err
}

func (s *BackupService) Operations() ([]BackupOperationEvent, error) {
	value, exists, err := s.settings.Get("backups_operation_log")
	if err != nil {
		return nil, err
	}
	if !exists || strings.TrimSpace(value) == "" {
		return []BackupOperationEvent{}, nil
	}
	var events []BackupOperationEvent
	if err := json.Unmarshal([]byte(value), &events); err != nil {
		return nil, errors.New("backup operation log is corrupted")
	}
	return events, nil
}

func (s *BackupService) recordOperation(action string, operationErr error) {
	events, _ := s.Operations()
	status, message := "success", "Completed successfully"
	if operationErr != nil {
		status, message = "error", operationErr.Error()
	}
	events = append([]BackupOperationEvent{{At: time.Now().UTC(), Action: action, Status: status, Message: message}}, events...)
	if len(events) > 20 {
		events = events[:20]
	}
	data, err := json.Marshal(events)
	if err == nil {
		_ = s.settings.Set("backups_operation_log", string(data))
	}
}

func (s *BackupService) createLocked(ctx context.Context, options BackupOptions, protectedIDs ...string) (*BackupRecord, error) {
	components, err := normalizeBackupComponents(options.Components)
	if err != nil {
		return nil, err
	}
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return nil, err
	}
	required, err := s.estimateBackupBytes(ctx, components)
	if err != nil {
		return nil, err
	}
	if err := checkBackupFreeSpace(directory, required); err != nil {
		return nil, err
	}
	id, err := platform.NewUUIDv7()
	if err != nil {
		return nil, err
	}
	createdAt := time.Now().UTC()
	manifest := BackupManifest{FormatVersion: backupFormatVersion, ID: id, CreatedAt: createdAt, AppVersion: CurrentVersion, Components: components}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	archivePath := filepath.Join(directory, id+".zip")
	temporaryArchive, err := os.CreateTemp(directory, "."+id+"-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary backup: %w", err)
	}
	temporaryPath := temporaryArchive.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = temporaryArchive.Close()
			_ = os.Remove(temporaryPath)
			_ = os.Remove(archivePath)
			_ = os.Remove(archivePath + ".sha256")
		}
	}()
	if err := temporaryArchive.Chmod(0600); err != nil {
		return nil, err
	}
	zipWriter := zip.NewWriter(temporaryArchive)
	if err := writeZipBytes(zipWriter, "manifest.json", manifestJSON); err != nil {
		zipWriter.Close()
		return nil, err
	}

	var snapshotPath string
	if hasBackupComponent(components, "panel") || hasBackupComponent(components, "mariadb") {
		snapshotPath, err = s.snapshotPanelDatabase()
		if err != nil {
			zipWriter.Close()
			return nil, fmt.Errorf("snapshot panel database: %w", err)
		}
		defer os.Remove(snapshotPath)
	}
	if hasBackupComponent(components, "panel") {
		if err := writeZipFile(zipWriter, "panel.db", snapshotPath); err != nil {
			zipWriter.Close()
			return nil, err
		}
		if err := writeZipFile(zipWriter, "config.yaml", s.configPath); err != nil {
			zipWriter.Close()
			return nil, fmt.Errorf("add panel config to backup: %w", err)
		}
	}
	if hasBackupComponent(components, "mariadb") {
		if err := s.writeMariaDBBackup(ctx, zipWriter, snapshotPath); err != nil {
			zipWriter.Close()
			return nil, err
		}
	}
	if hasBackupComponent(components, "websites") {
		if err := writeWebsiteFiles(zipWriter, "/var/www"); err != nil {
			zipWriter.Close()
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("finish backup archive: %w", err)
	}
	if err := temporaryArchive.Sync(); err != nil {
		return nil, err
	}
	if err := temporaryArchive.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(temporaryPath, archivePath); err != nil {
		return nil, fmt.Errorf("publish backup archive: %w", err)
	}
	checksum, err := hashFile(archivePath)
	if err != nil {
		return nil, err
	}
	if err := writeChecksumFile(archivePath, checksum); err != nil {
		return nil, err
	}
	cleanup = false
	if err := s.applyRetention(settings, id, protectedIDs...); err != nil {
		return nil, fmt.Errorf("backup was created but retention cleanup failed: %w", err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	return &BackupRecord{ID: id, CreatedAt: createdAt, AppVersion: CurrentVersion, Components: components, SizeBytes: info.Size(), Verified: true}, nil
}

func normalizeBackupComponents(components []string) ([]string, error) {
	if len(components) == 0 {
		return nil, errors.New("select at least one component to back up")
	}
	allowed := map[string]bool{"panel": true, "mariadb": true, "websites": true}
	selected := map[string]bool{}
	for _, component := range components {
		if !allowed[component] {
			return nil, fmt.Errorf("unsupported backup component: %s", component)
		}
		selected[component] = true
	}
	result := make([]string, 0, len(selected))
	for _, component := range []string{"panel", "mariadb", "websites"} {
		if selected[component] {
			result = append(result, component)
		}
	}
	return result, nil
}

func (s *BackupService) estimateBackupBytes(ctx context.Context, components []string) (uint64, error) {
	required := uint64(64 << 20)
	add := func(size uint64) error {
		if size > ^uint64(0)-required {
			return errors.New("backup size estimate overflowed")
		}
		required += size
		return nil
	}
	if hasBackupComponent(components, "panel") || hasBackupComponent(components, "mariadb") {
		for _, path := range []string{s.databasePath, s.databasePath + "-wal"} {
			info, err := os.Stat(path)
			if os.IsNotExist(err) && path != s.databasePath {
				continue
			}
			if err != nil {
				return 0, fmt.Errorf("inspect panel database size: %w", err)
			}
			if info.Mode().IsRegular() {
				if err := add(uint64(info.Size())); err != nil {
					return 0, err
				}
			}
		}
	}
	if hasBackupComponent(components, "websites") {
		if err := filepath.WalkDir("/var/www", func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == "/var/www/phpmyadmin" && entry.IsDir() {
				return filepath.SkipDir
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				return add(uint64(info.Size()))
			}
			return nil
		}); err != nil {
			return 0, fmt.Errorf("estimate website backup size: %w", err)
		}
	}
	if hasBackupComponent(components, "mariadb") {
		password, exists, err := s.settings.Get("mariadb_admin_password")
		if err != nil || !exists || password == "" {
			return 0, errors.New("MegoPanel MariaDB credentials are not configured")
		}
		output, err := platform.MariaDBQuery(ctx, "megopanel", password, "SELECT COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0) FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('information_schema','mysql','performance_schema','sys');")
		if err != nil {
			return 0, errors.New("could not estimate MariaDB backup size")
		}
		mariaBytes, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
		if err != nil {
			return 0, errors.New("MariaDB returned an invalid backup size estimate")
		}
		if err := add(mariaBytes); err != nil {
			return 0, err
		}
	}
	return required, nil
}

func (s *BackupService) snapshotPanelDatabase() (string, error) {
	file, err := os.CreateTemp("", "megopanel-panel-*.db")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if err := s.db.Exec("VACUUM INTO ?", path).Error; err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0600); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

type backupDatabaseUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *BackupService) writeMariaDBBackup(ctx context.Context, archive *zip.Writer, snapshotPath string) error {
	password, exists, err := s.settings.Get("mariadb_admin_password")
	if err != nil || !exists || password == "" {
		return errors.New("MegoPanel MariaDB credentials are not configured")
	}
	output, err := platform.MariaDBQuery(ctx, "megopanel", password, "SHOW DATABASES;")
	if err != nil {
		return errors.New("could not list MariaDB databases for backup")
	}
	databases := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || isBackupSystemDatabase(name) {
			continue
		}
		if !platformValidDatabaseName(name) {
			return errors.New("MariaDB returned an unsupported database name")
		}
		databases = append(databases, name)
	}
	sort.Strings(databases)
	entry, err := archive.CreateHeader(backupZipHeader("mariadb.sql", 0600))
	if err != nil {
		return err
	}
	if len(databases) == 0 {
		if _, err := io.WriteString(entry, "-- No application databases were present when this backup was created.\n"); err != nil {
			return err
		}
	} else if err := platform.MariaDBDump(ctx, "megopanel", password, databases, entry); err != nil {
		return fmt.Errorf("create consistent MariaDB backup: %w", err)
	}
	users := make([]backupDatabaseUser, 0)
	if snapshotPath != "" {
		if err := readBackupDatabaseUsers(snapshotPath, &users); err != nil {
			return fmt.Errorf("read saved database accounts: %w", err)
		}
	}
	usersData, err := json.Marshal(users)
	if err != nil {
		return err
	}
	return writeZipBytes(archive, "database-users.json", usersData)
}

func readBackupDatabaseUsers(path string, users *[]backupDatabaseUser) error {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	var settings []domain.Setting
	if err := db.Where("key LIKE ?", "db_password_%").Find(&settings).Error; err != nil {
		return err
	}
	for _, setting := range settings {
		name := strings.TrimPrefix(setting.Key, "db_password_")
		if platformValidDatabaseName(name) && setting.Value != "" {
			*users = append(*users, backupDatabaseUser{Name: name, Password: setting.Value})
		}
	}
	return nil
}

func writeWebsiteFiles(archive *zip.Writer, root string) error {
	if _, err := archive.CreateHeader(backupZipHeader("websites/", 0700)); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect website data directory: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("website data root must be a real directory")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == filepath.Join(root, "phpmyadmin") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		name := "websites/" + filepath.ToSlash(relative)
		if entry.IsDir() {
			_, err := archive.CreateHeader(backupZipHeader(name+"/", 0700))
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return writeZipFile(archive, name, path)
	})
}

func backupZipHeader(name string, mode os.FileMode) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	header.Modified = time.Now().UTC()
	return header
}

func writeZipBytes(archive *zip.Writer, name string, data []byte) error {
	entry, err := archive.CreateHeader(backupZipHeader(name, 0600))
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func writeZipFile(archive *zip.Writer, name, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to include non-regular backup source %s", path)
	}
	entry, err := archive.CreateHeader(backupZipHeader(name, 0600))
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(entry, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeChecksumFile(archivePath, checksum string) error {
	temporary, err := os.CreateTemp(filepath.Dir(archivePath), ".checksum-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := fmt.Fprintf(temporary, "%s  %s\n", checksum, filepath.Base(archivePath)); err != nil {
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
	return os.Rename(temporaryPath, archivePath+".sha256")
}

func ensureBackupDirectory(path string) (string, error) {
	if !allowedBackupDirectory(path) {
		return "", errors.New("backup directory is outside the supported storage locations")
	}
	clean := filepath.Clean(path)
	if err := rejectSymlinkPath(clean); err != nil {
		return "", err
	}
	if err := os.MkdirAll(clean, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup storage path must be a real directory")
	}
	if err := os.Chmod(clean, 0700); err != nil {
		return "", err
	}
	return clean, nil
}

func rejectSymlinkPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(os.PathSeparator)
	relative := strings.TrimPrefix(absolute, current)
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup path contains a symlink: %s", current)
		}
	}
	return nil
}

func checkBackupFreeSpace(path string, required uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("check backup storage space: %w", err)
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	if available < required {
		return fmt.Errorf("backup storage has %s available but at least %s is required", formatBackupBytes(available), formatBackupBytes(required))
	}
	return nil
}

func formatBackupBytes(size uint64) string {
	const mb = uint64(1 << 20)
	if size > uint64(1<<30) {
		return fmt.Sprintf("%.1f GB", float64(size)/float64(1<<30))
	}
	return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
}

func (s *BackupService) List() ([]BackupRecord, error) {
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	backups := make([]BackupRecord, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".zip")
		if !backupIDPattern.MatchString(id) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		manifest, err := readBackupManifest(path)
		if err != nil || manifest.ID != id {
			continue
		}
		backups = append(backups, BackupRecord{ID: id, CreatedAt: manifest.CreatedAt, AppVersion: manifest.AppVersion, Components: manifest.Components, SizeBytes: info.Size()})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	return backups, nil
}

func (s *BackupService) Verify(id string) (*BackupRecord, error) {
	if !backupIDPattern.MatchString(id) {
		return nil, errors.New("invalid backup id")
	}
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, id+".zip")
	if err := verifyBackupArchive(path); err != nil {
		return nil, err
	}
	manifest, err := readBackupManifest(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &BackupRecord{ID: id, CreatedAt: manifest.CreatedAt, AppVersion: manifest.AppVersion, Components: manifest.Components, SizeBytes: info.Size(), Verified: true}, nil
}

func (s *BackupService) DownloadPath(id string) (string, error) {
	if !backupIDPattern.MatchString(id) {
		return "", errors.New("invalid backup id")
	}
	settings, err := s.Settings()
	if err != nil {
		return "", err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, id+".zip")
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup archive must be a regular file")
	}
	return path, nil
}

func verifyBackupArchive(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup archive must be a regular file")
	}
	checksum, err := readChecksumFile(path)
	if err != nil {
		return err
	}
	actual, err := hashFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, checksum) {
		return errors.New("backup SHA-256 checksum does not match")
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("backup archive is not a valid zip file: %w", err)
	}
	defer archive.Close()
	var total uint64
	seen := make(map[string]struct{}, len(archive.File))
	for _, file := range archive.File {
		if _, duplicate := seen[file.Name]; duplicate {
			return errors.New("backup archive contains duplicate paths")
		}
		seen[file.Name] = struct{}{}
		if file.UncompressedSize64 > maxBackupEntrySize || file.UncompressedSize64 > maxBackupEntrySize-total {
			return errors.New("backup archive expands beyond the allowed size")
		}
		total += file.UncompressedSize64
		mode := file.FileInfo().Mode()
		if !validBackupEntryName(file.Name) || mode&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !mode.IsRegular()) {
			return errors.New("backup archive contains an unsafe path")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if copyErr != nil {
			return fmt.Errorf("read backup archive entry %s: %w", file.Name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	manifest, err := readBackupManifest(path)
	if err != nil {
		return err
	}
	if manifest.ID == "" || !backupIDPattern.MatchString(manifest.ID) || manifest.CreatedAt.IsZero() || len(manifest.AppVersion) == 0 {
		return errors.New("backup manifest is invalid")
	}
	components, err := normalizeBackupComponents(manifest.Components)
	if err != nil || len(components) != len(manifest.Components) {
		return errors.New("backup manifest contains invalid components")
	}
	for _, component := range components {
		switch component {
		case "panel":
			if _, ok := seen["panel.db"]; !ok {
				return errors.New("backup is missing its panel database")
			}
			if _, ok := seen["config.yaml"]; !ok {
				return errors.New("backup is missing its panel configuration")
			}
		case "mariadb":
			if _, ok := seen["mariadb.sql"]; !ok {
				return errors.New("backup is missing its MariaDB dump")
			}
			if _, ok := seen["database-users.json"]; !ok {
				return errors.New("backup is missing its database account metadata")
			}
		case "websites":
			if _, ok := seen["websites/"]; !ok {
				return errors.New("backup is missing its website data directory")
			}
		}
	}
	return nil
}

func readChecksumFile(path string) (string, error) {
	info, err := os.Lstat(path + ".sha256")
	if err != nil {
		return "", fmt.Errorf("backup checksum file is missing: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 256 {
		return "", errors.New("backup checksum file is invalid")
	}
	data, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return "", fmt.Errorf("backup checksum file is missing: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[1] != filepath.Base(path) || len(fields[0]) != sha256.Size*2 {
		return "", errors.New("backup checksum file is malformed")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("backup checksum is not hexadecimal")
	}
	return fields[0], nil
}

func readBackupManifest(path string) (*BackupManifest, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name != "manifest.json" {
			continue
		}
		if file.UncompressedSize64 > 1<<20 {
			return nil, errors.New("backup manifest is too large")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 1<<20+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		var manifest BackupManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		if manifest.FormatVersion < 1 || manifest.ID == "" || len(manifest.Components) == 0 {
			return nil, errors.New("backup manifest is incomplete")
		}
		return &manifest, nil
	}
	return nil, errors.New("backup manifest is missing")
}

func validBackupEntryName(name string) bool {
	if name == "manifest.json" || name == "panel.db" || name == "config.yaml" || name == "mariadb.sql" || name == "database-users.json" {
		return true
	}
	if name == "websites/" {
		return true
	}
	if !strings.HasPrefix(name, "websites/") || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "websites" || strings.HasSuffix(name, "//") {
		return false
	}
	components := strings.Split(strings.TrimPrefix(trimmed, "websites/"), "/")
	if len(components) > 0 && strings.EqualFold(components[0], "phpmyadmin") {
		return false
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func (s *BackupService) Delete(id string) error {
	if !backupIDPattern.MatchString(id) {
		err := errors.New("invalid backup id")
		s.recordOperation("delete", err)
		return err
	}
	if !systemOperationMu.TryLock() {
		err := errors.New("another backup or restore operation is already running")
		s.recordOperation("delete", err)
		return err
	}
	defer systemOperationMu.Unlock()
	err := s.deleteLocked(id)
	s.recordOperation("delete", err)
	return err
}

func (s *BackupService) deleteLocked(id string) error {
	settings, err := s.Settings()
	if err != nil {
		return err
	}
	directory, err := ensureBackupDirectory(settings.Directory)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, id+".zip")
	if info, err := os.Lstat(path); err != nil {
		return err
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup path is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.Remove(path + ".sha256"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *BackupService) applyRetention(settings BackupSettings, newestID string, protectedIDs ...string) error {
	backups, err := s.List()
	if err != nil {
		return err
	}
	if len(backups) <= 1 {
		return nil
	}
	protected := make(map[string]bool, len(protectedIDs))
	for _, id := range protectedIDs {
		protected[id] = true
	}
	for index, backup := range backups {
		if backup.ID == newestID || protected[backup.ID] || index == 0 {
			continue
		}
		overCount := index >= settings.RetentionCount
		tooOld := time.Since(backup.CreatedAt) > time.Duration(settings.RetentionDays)*24*time.Hour
		if overCount || tooOld {
			path := filepath.Join(settings.Directory, backup.ID+".zip")
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Remove(path + ".sha256"); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (s *BackupService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.runScheduledBackup(ctx, now)
			}
		}
	}()
}

func (s *BackupService) runScheduledBackup(ctx context.Context, now time.Time) {
	settings, err := s.Settings()
	if err != nil || !settings.ScheduleEnabled || now.Hour() < settings.ScheduleHour {
		return
	}
	day := now.Format("2006-01-02")
	lastAttempt, _, err := s.settings.Get("backups_last_attempt_day")
	if err != nil || lastAttempt == day {
		return
	}
	if err := s.settings.Set("backups_last_attempt_day", day); err != nil {
		return
	}
	components := make([]string, 0, len(settings.ScheduleComponents))
	for _, component := range settings.ScheduleComponents {
		switch component {
		case "mariadb":
			if password, exists, _ := s.settings.Get("mariadb_admin_password"); exists && password != "" {
				components = append(components, component)
			}
		case "websites":
			if info, err := os.Stat("/var/www"); err == nil && info.IsDir() {
				components = append(components, component)
			}
		default:
			components = append(components, component)
		}
	}
	if len(components) == 0 {
		return
	}
	if _, err := s.Create(ctx, BackupOptions{Components: components}); err != nil {
		return
	}
	_ = s.settings.Set("backups_last_run_day", day)
}

func platformValidDatabaseName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func isBackupSystemDatabase(name string) bool {
	switch strings.ToLower(name) {
	case "information_schema", "mysql", "performance_schema", "sys":
		return true
	default:
		return false
	}
}
