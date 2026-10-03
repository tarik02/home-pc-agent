//go:build !windows

package windows

import "errors"

var ErrUnsupported = errors.New("operation is only supported on Windows")

func LockWorkStation() error {
	return ErrUnsupported
}

func WorkstationLocked() (bool, error) {
	return false, ErrUnsupported
}

func Sleep() error {
	return ErrUnsupported
}

func DisplayOff() error {
	return ErrUnsupported
}
