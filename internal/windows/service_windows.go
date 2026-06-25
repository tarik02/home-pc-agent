//go:build windows

package windows

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func InstallService(name, displayName, exePath string, args []string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	if existing, err := manager.OpenService(name); err == nil {
		existing.Close()
		return fmt.Errorf("service %q already exists", name)
	}

	service, err := manager.CreateService(name, exePath, mgr.Config{
		DisplayName: displayName,
		StartType:   mgr.StartAutomatic,
		Description: "Home Assistant PC control agent",
	}, args...)
	if err != nil {
		return fmt.Errorf("create service %q: %w", name, err)
	}
	defer service.Close()
	return nil
}

func UninstallService(name string) error {
	service, err := openService(name)
	if err != nil {
		return err
	}
	defer service.Close()

	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete service %q: %w", name, err)
	}
	return nil
}

func StartService(name string) error {
	service, err := openService(name)
	if err != nil {
		return err
	}
	defer service.Close()
	if err := service.Start(); err != nil {
		return fmt.Errorf("start service %q: %w", name, err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query service %q: %w", name, err)
		}
		switch status.State {
		case svc.Running:
			return nil
		case svc.Stopped:
			return fmt.Errorf("start service %q: service stopped while starting", name)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("start service %q: timed out waiting for running state", name)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func StopService(name string) error {
	service, err := openService(name)
	if err != nil {
		return err
	}
	defer service.Close()

	status, err := service.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service %q: %w", name, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(deadline) {
			return fmt.Errorf("stop service %q: timed out", name)
		}
		time.Sleep(500 * time.Millisecond)
		status, err = service.Query()
		if err != nil {
			return fmt.Errorf("query service %q: %w", name, err)
		}
	}
	return nil
}

func openService(name string) (*mgr.Service, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(name)
	if err != nil {
		return nil, fmt.Errorf("open service %q: %w", name, err)
	}
	return service, nil
}
