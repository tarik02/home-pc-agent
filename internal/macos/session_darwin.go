//go:build darwin

package macos

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

const (
	loginFrameworkPath = "/System/Library/PrivateFrameworks/login.framework/Versions/A/login"
	pmsetPath          = "/usr/bin/pmset"
	ioregPath          = "/usr/sbin/ioreg"
	plutilPath         = "/usr/bin/plutil"
)

var resolveLockScreen = sync.OnceValues(func() (func(), error) {
	handle, err := purego.Dlopen(loginFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load login framework: %w", err)
	}
	symbol, err := purego.Dlsym(handle, "SACLockScreenImmediate")
	if err != nil {
		return nil, fmt.Errorf("resolve SACLockScreenImmediate: %w", err)
	}
	var lockScreen func()
	purego.RegisterFunc(&lockScreen, symbol)
	return lockScreen, nil
})

func CheckLockScreen() error {
	_, err := resolveLockScreen()
	return err
}

func LockScreen() error {
	lockScreen, err := resolveLockScreen()
	if err != nil {
		return err
	}
	lockScreen()
	return nil
}

func Sleep(ctx context.Context) error {
	return run(ctx, "sleep", pmsetPath, "sleepnow")
}

func DisplayOff(ctx context.Context) error {
	return run(ctx, "display off", pmsetPath, "displaysleepnow")
}

func SessionLocked(ctx context.Context) (bool, error) {
	registry, err := output(ctx, ioregPath, "-a", "-n", "Root", "-d1")
	if err != nil {
		return false, fmt.Errorf("read console state: %w", err)
	}

	cmd := exec.CommandContext(ctx, plutilPath, "-extract", "IOConsoleLocked", "raw", "-o", "-", "-")
	cmd.Stdin = bytes.NewReader(registry)
	value, err := commandOutput(cmd)
	if err != nil {
		return false, fmt.Errorf("decode console state: %w", err)
	}
	switch strings.TrimSpace(string(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected IOConsoleLocked value %q", strings.TrimSpace(string(value)))
	}
}

func run(ctx context.Context, action string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("%s: %w", action, err)
		}
		return fmt.Errorf("%s: %w: %s", action, err, detail)
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return commandOutput(exec.CommandContext(ctx, name, args...))
}

func commandOutput(cmd *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	value, err := cmd.Output()
	if err == nil {
		return value, nil
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", err, detail)
}
