//go:build !windows

package windows

import "context"

func RunningAsService() (bool, error) {
	return false, nil
}

func RunAgentService(name string, run func(ctx context.Context) error) error {
	return ErrUnsupported
}

func RunAgentServiceDebug(name string, run func(ctx context.Context) error) error {
	return ErrUnsupported
}
