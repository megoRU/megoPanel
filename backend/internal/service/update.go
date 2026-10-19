package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	CurrentVersion      = "v1.2.0"
	GitHubAPIURL        = "https://api.github.com/repos/megoRU/megoPanel/releases/latest"
	maxReleaseAPIBytes  = 1 << 20
	maxChecksumFileSize = 4096
	maxArchiveSize      = 256 << 20
	maxExtractedSize    = 512 << 20
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type UpdateStatus struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
}

type UpdateService struct{}

func NewUpdateService() *UpdateService { return &UpdateService{} }

func (s *UpdateService) CheckUpdate(ctx context.Context) (*UpdateStatus, error) {
	client := newReleaseHTTPClient(10 * time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, GitHubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "MegoPanel-Updater")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return &UpdateStatus{CurrentVersion: CurrentVersion, LatestVersion: CurrentVersion}, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", response.StatusCode)
	}
	var release GitHubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReleaseAPIBytes)).Decode(&release); err != nil {
		return nil, err
	}
	latest := strings.TrimSpace(release.TagName)
	return &UpdateStatus{CurrentVersion: CurrentVersion, LatestVersion: latest, HasUpdate: isStableVersion(latest) && isNewerVersion(CurrentVersion, latest)}, nil
}

// Upgrade downloads a checksummed release and stages every file before replacing
// the live binary or templates. Restart is queued through systemd after the
// current HTTP response has had time to return.
func (s *UpdateService) Upgrade(ctx context.Context) error {
	client := newReleaseHTTPClient(90 * time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, GitHubAPIURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "MegoPanel-Updater")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status %d", response.StatusCode)
	}
	var release GitHubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReleaseAPIBytes)).Decode(&release); err != nil {
		return err
	}
	if !isStableVersion(strings.TrimSpace(release.TagName)) || !isNewerVersion(CurrentVersion, strings.TrimSpace(release.TagName)) {
		return errors.New("a newer stable release is not available")
	}

	assets := make(map[string]string, len(release.Assets))
	for _, asset := range release.Assets {
		if asset.Name == "megopanel.tar.gz" || asset.Name == "megopanel.tar.gz.sha256" {
			assets[asset.Name] = asset.BrowserDownloadURL
		}
	}
	archiveURL, checksumURL := assets["megopanel.tar.gz"], assets["megopanel.tar.gz.sha256"]
	if archiveURL == "" || checksumURL == "" {
		return errors.New("release package or SHA-256 checksum is missing")
	}
	checksumBytes, err := downloadSmall(ctx, client, checksumURL, maxChecksumFileSize)
	if err != nil {
		return fmt.Errorf("download release checksum: %w", err)
	}
	checksumFields := strings.Fields(string(checksumBytes))
	if len(checksumFields) == 0 || len(checksumFields[0]) != sha256.Size*2 {
		return errors.New("release checksum file has an invalid format")
	}
	expectedChecksum, err := hex.DecodeString(checksumFields[0])
	if err != nil {
		return errors.New("release checksum is not valid hexadecimal")
	}

	tempDir, err := os.MkdirTemp("", "megopanel-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, "megopanel.tar.gz")
	if err := downloadVerifiedArchive(ctx, client, archiveURL, archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("download release package: %w", err)
	}
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.Mkdir(extractDir, 0700); err != nil {
		return err
	}
	if err := untar(archivePath, extractDir); err != nil {
		return fmt.Errorf("extract release package: %w", err)
	}

	newBinary := filepath.Join(extractDir, "backend", "megopanel")
	if info, err := os.Lstat(newBinary); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("release package does not contain a regular backend binary")
	}
	newTemplates := filepath.Join(extractDir, "backend", "templates")
	if info, err := os.Lstat(newTemplates); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("release package does not contain a regular backend templates directory")
	}

	binPath := "/usr/local/bin/megopanel"
	binInfo, err := os.Lstat(binPath)
	if err != nil || !binInfo.Mode().IsRegular() || binInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed MegoPanel binary is missing or is not a regular file")
	}
	optDir := "/opt/megopanel/backend"
	templatePath := filepath.Join(optDir, "templates")
	templateInfo, err := os.Lstat(templatePath)
	if err != nil || !templateInfo.IsDir() || templateInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed MegoPanel templates directory is missing or is not a regular directory")
	}

	stagedBinary, err := stageFile(newBinary, filepath.Dir(binPath), 0755)
	if err != nil {
		return fmt.Errorf("stage updated binary: %w", err)
	}
	defer os.Remove(stagedBinary)
	stagedTemplates, err := os.MkdirTemp(optDir, ".templates-new-*")
	if err != nil {
		return fmt.Errorf("stage updated templates: %w", err)
	}
	defer os.RemoveAll(stagedTemplates)
	if err := exec.CommandContext(ctx, "rsync", "-a", "--delete", newTemplates+string(os.PathSeparator), stagedTemplates+string(os.PathSeparator)).Run(); err != nil {
		return fmt.Errorf("stage updated frontend templates: %w", err)
	}

	backupBinary := binPath + ".previous"
	if info, err := os.Lstat(backupBinary); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("previous binary backup path is not a regular file")
		}
		if err := os.Remove(backupBinary); err != nil {
			return fmt.Errorf("remove previous binary backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	backupStage, err := stageFile(binPath, filepath.Dir(binPath), 0755)
	if err != nil {
		return fmt.Errorf("save current binary for rollback: %w", err)
	}
	if err := os.Rename(backupStage, backupBinary); err != nil {
		os.Remove(backupStage)
		return fmt.Errorf("save current binary for rollback: %w", err)
	}

	backupTemplates := filepath.Join(optDir, "templates.previous")
	if info, err := os.Lstat(backupTemplates); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("previous templates backup path is not a regular directory")
		}
		if err := os.RemoveAll(backupTemplates); err != nil {
			return fmt.Errorf("remove previous templates backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(templatePath, backupTemplates); err != nil {
		return fmt.Errorf("save current templates for rollback: %w", err)
	}
	if err := os.Rename(stagedTemplates, templatePath); err != nil {
		_ = os.Rename(backupTemplates, templatePath)
		return fmt.Errorf("activate updated templates: %w", err)
	}
	if err := os.Rename(stagedBinary, binPath); err != nil {
		_ = os.RemoveAll(templatePath)
		_ = os.Rename(backupTemplates, templatePath)
		return fmt.Errorf("activate updated binary: %w", err)
	}

	unit := "megopanel-restart-" + strconv.FormatInt(time.Now().Unix(), 10)
	if err := exec.CommandContext(ctx, "systemd-run", "--quiet", "--unit="+unit, "--on-active=2s", "/bin/systemctl", "restart", "megopanel").Run(); err != nil {
		rollbackErr := rollbackUpdate(binPath, backupBinary, templatePath, backupTemplates)
		if rollbackErr != nil {
			return fmt.Errorf("schedule service restart: %v; rollback failed: %w", err, rollbackErr)
		}
		return fmt.Errorf("schedule service restart: %w", err)
	}
	return nil
}

func downloadSmall(ctx context.Context, client *http.Client, rawURL string, maxSize int64) ([]byte, error) {
	if err := validateReleaseURL(rawURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := validateReleaseURL(response.Request.URL.String()); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("download exceeds the allowed size")
	}
	return data, nil
}

func downloadVerifiedArchive(ctx context.Context, client *http.Client, rawURL, target string, expected []byte) error {
	if err := validateReleaseURL(rawURL); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := validateReleaseURL(response.Request.URL.String()); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", response.StatusCode)
	}
	if response.ContentLength > maxArchiveSize {
		return errors.New("release archive exceeds the allowed size")
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxArchiveSize+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxArchiveSize {
		return errors.New("release archive exceeds the allowed size")
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return errors.New("release archive SHA-256 checksum does not match")
	}
	return nil
}

func validateReleaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" && parsed.Port() != "443" {
		return errors.New("release asset URL must use HTTPS")
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return errors.New("release asset URL is not hosted by GitHub")
	}
}

func newReleaseHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("release server redirected too many times")
			}
			return validateReleaseURL(request.URL.String())
		},
	}
}

func rollbackUpdate(binaryPath, binaryBackup, templatesPath, templatesBackup string) error {
	rollbackBinary, err := stageFile(binaryBackup, filepath.Dir(binaryPath), 0755)
	if err != nil {
		return err
	}
	defer os.Remove(rollbackBinary)
	if err := os.Rename(rollbackBinary, binaryPath); err != nil {
		return err
	}
	if err := os.RemoveAll(templatesPath); err != nil {
		return err
	}
	return os.Rename(templatesBackup, templatesPath)
}

func isNewerVersion(current, latest string) bool {
	currentParts, currentPre, currentOK := parseVersion(current)
	latestParts, latestPre, latestOK := parseVersion(latest)
	if !currentOK || !latestOK {
		return false
	}
	length := len(currentParts)
	if len(latestParts) > length {
		length = len(latestParts)
	}
	for index := 0; index < length; index++ {
		currentPart, latestPart := 0, 0
		if index < len(currentParts) {
			currentPart = currentParts[index]
		}
		if index < len(latestParts) {
			latestPart = latestParts[index]
		}
		if latestPart != currentPart {
			return latestPart > currentPart
		}
	}
	if currentPre == latestPre {
		return false
	}
	return currentPre != "" && latestPre == ""
}

func isStableVersion(version string) bool {
	_, prerelease, ok := parseVersion(version)
	return ok && prerelease == ""
}

func parseVersion(version string) ([]int, string, bool) {
	version = cleanVersion(version)
	if version == "" || strings.Contains(version, "+") {
		return nil, "", false
	}
	coreAndPre := strings.SplitN(version, "-", 2)
	parts := strings.Split(coreAndPre[0], ".")
	if len(parts) < 1 || len(parts) > 4 {
		return nil, "", false
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return nil, "", false
		}
		values[i] = value
	}
	pre := ""
	if len(coreAndPre) == 2 {
		pre = coreAndPre[1]
		if pre == "" {
			return nil, "", false
		}
	}
	return values, pre, true
}

func cleanVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return strings.TrimSpace(version)
}

func untar(tarball, targetDir string) error {
	reader, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer reader.Close()
	archive, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer archive.Close()
	targetAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(archive)
	var extractedSize int64
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Name == "" || strings.Contains(header.Name, "\\") || filepath.IsAbs(header.Name) {
			return errors.New("archive contains an invalid path")
		}
		cleanName := filepath.Clean(filepath.FromSlash(header.Name))
		path := filepath.Join(targetAbs, cleanName)
		relative, err := filepath.Rel(targetAbs, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("archive path escapes the extraction directory")
		}
		if header.Size < 0 || header.Size > maxExtractedSize-extractedSize {
			return errors.New("archive expands beyond the allowed size")
		}
		extractedSize += header.Size
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0755
			if mode == 0 {
				mode = 0644
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, tarReader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("archive contains an unsupported file type")
		}
	}
	return nil
}

func stageFile(sourcePath, destinationDir string, mode os.FileMode) (string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	staged, err := os.CreateTemp(destinationDir, ".megopanel-stage-*")
	if err != nil {
		return "", err
	}
	stagedPath := staged.Name()
	if _, err := io.Copy(staged, source); err != nil {
		staged.Close()
		os.Remove(stagedPath)
		return "", err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		os.Remove(stagedPath)
		return "", err
	}
	if err := staged.Chmod(mode); err != nil {
		staged.Close()
		os.Remove(stagedPath)
		return "", err
	}
	if err := staged.Close(); err != nil {
		os.Remove(stagedPath)
		return "", err
	}
	return stagedPath, nil
}
