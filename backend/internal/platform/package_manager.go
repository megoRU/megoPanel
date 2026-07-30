package platform

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

type PackageManager interface {
	Install(packageName string) error
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
func (m *AptManager) Install(p string) error {
	if err := exec.Command("apt-get", "update").Run(); err != nil {
		return err
	}
	return exec.Command("apt-get", "install", "-y", p).Run()
}
func (m *AptManager) Restart(s string) error { return exec.Command("systemctl", "restart", s).Run() }
func (m *AptManager) Enable(s string) error {
	return exec.Command("systemctl", "enable", "--now", s).Run()
}
