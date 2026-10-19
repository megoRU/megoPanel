package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"mego-panel/backend/internal/domain"
)

const managedWebsiteMarker = "# Managed by MegoPanel."

var managedWebsiteDomain = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$`)
var nginxServerNameDirective = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)

type WebsiteService struct {
	db             *gorm.DB
	webRoot        string
	sitesAvailable string
	sitesEnabled   string
	nginxBinary    string
	systemctl      string
}

func NewWebsiteService(db *gorm.DB) *WebsiteService {
	return newWebsiteService(db, "/var/www", "/etc/nginx/sites-available", "/etc/nginx/sites-enabled", "nginx", "systemctl")
}

func newWebsiteService(db *gorm.DB, webRoot, sitesAvailable, sitesEnabled, nginxBinary, systemctl string) *WebsiteService {
	return &WebsiteService{db: db, webRoot: webRoot, sitesAvailable: sitesAvailable, sitesEnabled: sitesEnabled, nginxBinary: nginxBinary, systemctl: systemctl}
}

func (s *WebsiteService) List() ([]domain.Website, error) {
	var websites []domain.Website
	if err := s.db.Order("created_at DESC").Find(&websites).Error; err != nil {
		return nil, errors.New("could not load websites")
	}
	return websites, nil
}

func (s *WebsiteService) Create(ctx context.Context, hostname, ipAddress string) (*domain.Website, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	ipAddress = strings.TrimSpace(ipAddress)
	if len(hostname) > 253 || !managedWebsiteDomain.MatchString(hostname) {
		return nil, errors.New("domain must be a valid DNS hostname")
	}
	if hostname == "phpmyadmin" {
		return nil, errors.New("phpMyAdmin's reserved website directory cannot be used as a site domain")
	}
	if net.ParseIP(ipAddress) == nil {
		return nil, errors.New("a valid website IP address is required")
	}
	systemOperationMu.Lock()
	defer systemOperationMu.Unlock()
	var count int64
	if err := s.db.Model(&domain.Website{}).Where("domain = ?", hostname).Count(&count).Error; err != nil {
		return nil, errors.New("could not check whether the website already exists")
	}
	if count > 0 {
		return nil, errors.New("a website with this domain is already registered")
	}
	if err := ensureRealDirectory(s.webRoot); err != nil {
		return nil, fmt.Errorf("website root is unavailable: %w", err)
	}
	if err := ensureRealDirectory(s.sitesAvailable); err != nil {
		return nil, fmt.Errorf("Nginx sites-available directory is unavailable: %w", err)
	}
	if err := ensureRealDirectory(s.sitesEnabled); err != nil {
		return nil, fmt.Errorf("Nginx sites-enabled directory is unavailable: %w", err)
	}
	if _, err := exec.LookPath(s.nginxBinary); err != nil {
		return nil, errors.New("Nginx is not installed; install and start it before adding a website")
	}
	if occupied, err := s.serverNameInUse(ctx, hostname); err != nil {
		return nil, err
	} else if occupied {
		return nil, errors.New("this domain is already configured in Nginx")
	}

	sitePath := filepath.Join(s.webRoot, hostname)
	if err := rejectSymlinkPath(sitePath); err != nil {
		return nil, err
	}
	createdDirectory := false
	if err := os.Mkdir(sitePath, 0755); err != nil {
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create website directory: %w", err)
		}
		info, statErr := os.Lstat(sitePath)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("website directory must be a real directory")
		}
		if info.Mode().Perm()&0005 != 0005 {
			return nil, errors.New("website directory must allow Nginx to traverse and read files")
		}
	} else {
		createdDirectory = true
		if err := os.Chmod(sitePath, 0755); err != nil {
			_ = os.Remove(sitePath)
			return nil, fmt.Errorf("set website directory permissions: %w", err)
		}
	}
	indexPath := filepath.Join(sitePath, "index.html")
	createdIndex := false
	if info, err := os.Lstat(indexPath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			if createdDirectory {
				_ = os.Remove(sitePath)
			}
			return nil, errors.New("website index path is not a regular file")
		}
		if info.Mode().Perm()&0004 == 0 {
			return nil, errors.New("website index file must be readable by Nginx")
		}
	} else if os.IsNotExist(err) {
		page := "<!doctype html>\n<html lang=\"en\"><meta charset=\"utf-8\"><title>" + hostname + "</title><body><h1>" + hostname + "</h1><p>Your website is ready. Add your site files to this directory.</p></body></html>\n"
		if err := writeExclusiveFile(indexPath, []byte(page), 0644); err != nil {
			if createdDirectory {
				_ = os.Remove(sitePath)
			}
			return nil, fmt.Errorf("create website start page: %w", err)
		}
		createdIndex = true
	} else {
		return nil, err
	}

	configPath := filepath.Join(s.sitesAvailable, hostname+".conf")
	enabledPath := filepath.Join(s.sitesEnabled, hostname+".conf")
	configCreated := false
	linkCreated := false
	cleanup := func() {
		if linkCreated {
			_ = os.Remove(enabledPath)
		}
		if configCreated {
			_ = os.Remove(configPath)
		}
		if createdIndex {
			_ = os.Remove(indexPath)
		}
		if createdDirectory {
			_ = os.Remove(sitePath)
		}
	}
	if err := writeExclusiveFile(configPath, []byte(websiteNginxConfig(hostname, sitePath)), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write Nginx website config: %w", err)
	}
	configCreated = true
	if err := os.Symlink(configPath, enabledPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("enable Nginx website config: %w", err)
	}
	linkCreated = true
	if err := s.validateAndReload(ctx); err != nil {
		cleanup()
		_ = s.validateAndReload(ctx)
		return nil, err
	}

	website := &domain.Website{Domain: hostname, IPAddress: ipAddress, Path: sitePath}
	if err := s.db.Create(website).Error; err != nil {
		cleanup()
		_ = s.validateAndReload(ctx)
		return nil, errors.New("Nginx website was rolled back because its record could not be saved")
	}
	return website, nil
}

func (s *WebsiteService) Delete(ctx context.Context, id uint64) error {
	systemOperationMu.Lock()
	defer systemOperationMu.Unlock()
	var website domain.Website
	if err := s.db.First(&website, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("website not found")
		}
		return errors.New("could not load website")
	}
	if !managedWebsiteDomain.MatchString(website.Domain) {
		return errors.New("stored website domain is invalid")
	}
	if err := ensureRealDirectory(s.sitesAvailable); err != nil {
		return err
	}
	if err := ensureRealDirectory(s.sitesEnabled); err != nil {
		return err
	}
	configPath := filepath.Join(s.sitesAvailable, website.Domain+".conf")
	enabledPath := filepath.Join(s.sitesEnabled, website.Domain+".conf")
	configInfo, configErr := os.Lstat(configPath)
	if os.IsNotExist(configErr) {
		return s.db.Delete(&domain.Website{}, id).Error
	}
	if configErr != nil {
		return configErr
	}
	if !configInfo.Mode().IsRegular() || configInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to remove a non-regular Nginx website config")
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(config), managedWebsiteMarker) {
		return errors.New("Nginx config is not managed by MegoPanel; it was left unchanged")
	}
	enabledInfo, err := os.Lstat(enabledPath)
	if err == nil {
		if enabledInfo.Mode()&os.ModeSymlink == 0 {
			return errors.New("refusing to remove a non-symlink Nginx site entry")
		}
		target, err := os.Readlink(enabledPath)
		if err != nil || filepath.Clean(target) != filepath.Clean(configPath) {
			return errors.New("Nginx site entry points to a different config; it was left unchanged")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(enabledPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(configPath); err != nil {
		_ = os.Symlink(configPath, enabledPath)
		return err
	}
	rollback := func() {
		_ = writeExclusiveFile(configPath, config, 0644)
		if enabledInfo != nil {
			_ = os.Symlink(configPath, enabledPath)
		}
		_ = s.validateAndReload(ctx)
	}
	if err := s.validateAndReload(ctx); err != nil {
		rollback()
		return err
	}
	if err := s.db.Delete(&domain.Website{}, id).Error; err != nil {
		rollback()
		return errors.New("website config was restored because its record could not be removed")
	}
	return nil
}

func (s *WebsiteService) validateAndReload(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, s.nginxBinary, "-t").CombinedOutput()
	if err != nil {
		return fmt.Errorf("Nginx rejected the website configuration: %s", strings.TrimSpace(string(output)))
	}
	if err := exec.CommandContext(ctx, s.systemctl, "reload", "nginx").Run(); err != nil {
		return fmt.Errorf("Nginx configuration is valid but reload failed: %w", err)
	}
	return nil
}

func (s *WebsiteService) serverNameInUse(ctx context.Context, hostname string) (bool, error) {
	output, err := exec.CommandContext(ctx, s.nginxBinary, "-T").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("could not inspect the current Nginx configuration: %w", err)
	}
	for _, match := range nginxServerNameDirective.FindAllStringSubmatch(string(output), -1) {
		for _, configured := range strings.Fields(match[1]) {
			configured = strings.TrimSuffix(configured, ".")
			if nginxNameMatches(configured, hostname) {
				return true, nil
			}
		}
	}
	return false, nil
}

func nginxNameMatches(configured, hostname string) bool {
	configured = strings.ToLower(configured)
	hostname = strings.ToLower(hostname)
	if configured == hostname {
		return true
	}
	if strings.HasPrefix(configured, "*.") {
		suffix := configured[1:]
		return strings.HasSuffix(hostname, suffix) && len(hostname) > len(suffix)
	}
	if strings.HasPrefix(configured, ".") {
		return hostname == configured[1:] || strings.HasSuffix(hostname, configured)
	}
	return false
}

func ensureRealDirectory(path string) error {
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path must be a real directory")
	}
	return nil
}

func writeExclusiveFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if syncErr != nil {
		_ = os.Remove(path)
		return syncErr
	}
	return closeErr
}

func websiteNginxConfig(hostname, root string) string {
	return managedWebsiteMarker + "\nserver {\n" +
		"    listen 80;\n    listen [::]:80;\n" +
		"    server_name " + hostname + ";\n" +
		"    root " + root + ";\n    index index.html index.htm;\n" +
		"    location / { try_files $uri $uri/ =404; }\n" +
		"    location ~* \\.php$ { return 404; }\n" +
		"    location ~ /\\. { deny all; }\n}\n"
}
