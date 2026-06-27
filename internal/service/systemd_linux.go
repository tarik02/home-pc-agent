//go:build linux

package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tarik02/home-pc-agent/internal/service/systemdunit"
)

func Install(name, displayName, exePath string, args []string) error {
	if err := systemdunit.ValidateServiceName(name); err != nil {
		return fmt.Errorf("service name: %w", err)
	}
	unitPath, err := userUnitPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user config dir: %w", err)
	}
	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("service unit %q already exists", unitPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat service unit %q: %w", unitPath, err)
	}

	execStart := append([]string{exePath}, args...)
	content, err := systemdunit.Render(systemdunit.Unit{
		Description: displayName,
		ExecStart:   execStart,
	})
	if err != nil {
		return fmt.Errorf("render service unit: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write service unit %q: %w", unitPath, err)
	}
	return daemonReload()
}

func Uninstall(name string) error {
	if err := systemdunit.ValidateServiceName(name); err != nil {
		return fmt.Errorf("service name: %w", err)
	}
	unitPath, err := userUnitPath(name)
	if err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "stop", unitName(name)).Run()
	_ = exec.Command("systemctl", "--user", "disable", unitName(name)).Run()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service unit %q: %w", unitPath, err)
	}
	return daemonReload()
}

func Start(name string) error {
	if err := systemdunit.ValidateServiceName(name); err != nil {
		return fmt.Errorf("service name: %w", err)
	}
	cmd := exec.Command("systemctl", "--user", "start", unitName(name))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start service %q: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func Stop(name string) error {
	if err := systemdunit.ValidateServiceName(name); err != nil {
		return fmt.Errorf("service name: %w", err)
	}
	cmd := exec.Command("systemctl", "--user", "stop", unitName(name))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stop service %q: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func userUnitPath(name string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "systemd", "user", unitName(name)), nil
}

func unitName(name string) string {
	return name + ".service"
}

func daemonReload() error {
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
