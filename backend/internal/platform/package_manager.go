package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type PackageManager interface {
	Install(packageName string) error
	InstallMany(packageNames []string) error
	Restart(serviceName string) error
	Enable(serviceName string) error
}
type AptManager struct{}

func DetectPackageManager() (PackageManager, error) {
	data, _ := os.ReadFile("/etc/os-release")
	text := strings.ToLower(string(data))
	if strings.Contains(text, "debian") || strings.Contains(text, "ubuntu") {
		return &AptManager{}, nil
	}
	return nil, errors.New("unsupported Linux distribution; Debian and Ubuntu are supported")
}
func (m *AptManager) Install(packageName string) error {
	return m.InstallMany([]string{packageName})
}

func (m *AptManager) InstallMany(packageNames []string) error {
	if len(packageNames) == 0 {
		return nil
	}
	resetPHPFpmStartLimit()
	_ = runCommand("dpkg", "--configure", "-a")
	resetPHPFpmStartLimit()
	_ = runCommand("apt-get", "update")
	arguments := []string{"install", "-y", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	arguments = append(arguments, packageNames...)
	err := runCommand("apt-get", arguments...)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "start-limit-hit") && !strings.Contains(err.Error(), "attempted too often") {
		return err
	}
	resetPHPFpmStartLimit()
	_ = runCommand("dpkg", "--configure", "-a")
	return runCommand("apt-get", arguments...)
}
func (m *AptManager) Restart(serviceName string) error {
	return runCommand("systemctl", "restart", serviceName)
}
func (m *AptManager) Enable(serviceName string) error {
	if strings.HasPrefix(serviceName, "php") && strings.HasSuffix(serviceName, "-fpm") {
		_ = runCommand("systemctl", "reset-failed", serviceName)
	}
	return runCommand("systemctl", "enable", "--now", serviceName)
}

func resetPHPFpmStartLimit() {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return
	}
	_ = runCommand("sh", "-c", "systemctl reset-failed 'php*-fpm.service' php-fpm.service 2>/dev/null || true")
}
func runCommand(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	command.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
