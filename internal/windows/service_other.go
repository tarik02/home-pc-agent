//go:build !windows

package windows

func InstallService(name, displayName, exePath string, args []string) error {
	return ErrUnsupported
}

func UninstallService(name string) error {
	return ErrUnsupported
}

func StartService(name string) error {
	return ErrUnsupported
}

func StopService(name string) error {
	return ErrUnsupported
}
