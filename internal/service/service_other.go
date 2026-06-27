//go:build !linux && !windows

package service

import "errors"

var ErrUnsupported = errors.New("service management is not supported on this platform")

func Install(name, displayName, exePath string, args []string) error {
	return ErrUnsupported
}

func Uninstall(name string) error {
	return ErrUnsupported
}

func Start(name string) error {
	return ErrUnsupported
}

func Stop(name string) error {
	return ErrUnsupported
}
