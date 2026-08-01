package service

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	CurrentVersion = "v1.0.0"
	GitHubAPIURL   = "https://api.github.com/repos/megoRU/megoPanel/releases/latest"
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

func NewUpdateService() *UpdateService {
	return &UpdateService{}
}

// CheckUpdate queries the GitHub Release API to see if a newer version is available.
func (s *UpdateService) CheckUpdate() (*UpdateStatus, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MegoPanel-Updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}

	latest := strings.TrimSpace(rel.TagName)
	hasUpdate := isNewerVersion(CurrentVersion, latest)

	return &UpdateStatus{
		CurrentVersion: CurrentVersion,
		LatestVersion:  latest,
		HasUpdate:      hasUpdate,
	}, nil
}

// Upgrade downloads the latest tarball, extracts it, unlinks and replaces the running binary,
// updates static frontend templates, and restarts the systemd service.
func (s *UpdateService) Upgrade() error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MegoPanel-Updater")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}

	latest := strings.TrimSpace(rel.TagName)
	if !isNewerVersion(CurrentVersion, latest) {
		return errors.New("already on the latest version")
	}

	var downloadURL string
	for _, asset := range rel.Assets {
		if asset.Name == "megopanel.tar.gz" {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return errors.New("megopanel.tar.gz package asset not found in latest release")
	}

	// Download release archive
	archiveResp, err := client.Get(downloadURL)
	if err != nil {
		return err
	}
	defer archiveResp.Body.Close()

	if archiveResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download release: status %d", archiveResp.StatusCode)
	}

	tempDir, err := os.MkdirTemp("", "megopanel-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	archivePath := filepath.Join(tempDir, "megopanel.tar.gz")
	out, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, archiveResp.Body); err != nil {
		return err
	}
	out.Close()

	// Extract the downloaded archive
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return err
	}

	if err := untar(archivePath, extractDir); err != nil {
		return err
	}

	// Upgrade the backend binary: replace /usr/local/bin/megopanel
	// Note: We MUST unlink (delete or rename) the running binary first to prevent "text file busy" errors.
	binPath := "/usr/local/bin/megopanel"
	newBinPath := filepath.Join(extractDir, "backend", "megopanel")

	if _, err := os.Stat(newBinPath); err == nil {
		// Prepare upgrade
		if _, err := os.Stat(binPath); err == nil {
			_ = os.Remove(binPath) // Unlink the running binary
		}

		if err := copyFile(newBinPath, binPath); err != nil {
			return fmt.Errorf("failed to replace backend binary: %w", err)
		}
		_ = os.Chmod(binPath, 0755)
	}

	// Synchronize files under /opt/megopanel
	optDir := "/opt/megopanel"
	if _, err := os.Stat(optDir); err == nil {
		// Use rsync to update backend templates/configs
		newTemplatesDir := filepath.Join(extractDir, "backend", "templates")
		if _, err := os.Stat(newTemplatesDir); err == nil {
			_ = exec.Command("rsync", "-a", "--delete", newTemplatesDir+"/", filepath.Join(optDir, "backend", "templates/")).Run()
		}
	}

	// Schedule restart in a separate goroutine
	go func() {
		time.Sleep(2 * time.Second)
		_ = exec.Command("systemctl", "restart", "megopanel").Run()
	}()

	return nil
}

// Helpers

func isNewerVersion(current, latest string) bool {
	c := cleanVersion(current)
	l := cleanVersion(latest)
	if c == "" || l == "" {
		return false
	}
	return c != l
}

func cleanVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	return strings.TrimSpace(v)
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

	tarReader := tar.NewReader(archive)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		path := filepath.Join(targetDir, header.Name)
		info := header.FileInfo()

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, info.Mode()); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return err
			}
			file.Close()
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
