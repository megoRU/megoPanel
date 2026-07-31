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
	if err := runCommand("dpkg", "--configure", "-a"); err != nil {
		return err
	}
	if err := runCommand("apt-get", "update"); err != nil {
		return err
	}
	return runCommand("apt-get", "install", "-y", packageName)
}
func (m *AptManager) Restart(serviceName string) error {
	return runCommand("systemctl", "restart", serviceName)
}
func (m *AptManager) Enable(serviceName string) error {
	return runCommand("systemctl", "enable", "--now", serviceName)
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
