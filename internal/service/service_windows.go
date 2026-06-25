//go:build windows

package service

import win "github.com/tarik02/home-pc-agent/internal/windows"

func Install(name, displayName, exePath string, args []string) error {
	return win.InstallService(name, displayName, exePath, args)
}

func Uninstall(name string) error {
	return win.UninstallService(name)
}

func Start(name string) error {
	return win.StartService(name)
}

func Stop(name string) error {
	return win.StopService(name)
}
