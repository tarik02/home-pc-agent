package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunCommandCapturesOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "echo.ps1")
	require.NoError(t, os.WriteFile(script, []byte(`Write-Output "hello"`), 0o600))

	result, err := runCommand(context.Background(), CommandSpec{
		Interpreter: interpreterPwsh,
		Path:        script,
		Delivery:    deliveryArgs,
		Output:      OutputConfig{Capture: true, MaxBytes: 1024},
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)
	require.Contains(t, result.Stdout, "hello")
}

func TestRunCommandPassesStructuredArgs(t *testing.T) {
	script := filepath.Join(t.TempDir(), "args.ps1")
	require.NoError(t, os.WriteFile(script, []byte(`
param([string]$Mode)
Write-Output $Mode
`), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := runCommand(ctx, CommandSpec{
		Interpreter: interpreterPwsh,
		Path:        script,
		Delivery:    deliveryArgs,
		Params:      map[string]any{"Mode": "gaming-chair"},
		Output:      OutputConfig{Capture: true, MaxBytes: 1024},
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)
	require.Contains(t, result.Stdout, "gaming-chair")
}

func TestBuildInvocationCommandWrapper(t *testing.T) {
	wrapper := filepath.Join(t.TempDir(), "wrapper.exe")
	require.NoError(t, os.WriteFile(wrapper, []byte("stub"), 0o600))

	name, args, _, _, err := buildInvocation(CommandSpec{
		Command: wrapper,
		Args: []string{
			"--cwd", `C:\Users\alice\.bin`,
			"--stdio", "--",
			"pwsh", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
		},
		Path:       `C:\scripts\display-scale.ps1`,
		StaticArgs: []string{"-DisplayName", "PG32UCDM", "-Query"},
		Delivery:   deliveryArgs,
	})
	require.NoError(t, err)
	require.Equal(t, wrapper, name)
	require.Equal(t, []string{
		"--cwd", `C:\Users\alice\.bin`,
		"--stdio", "--",
		"pwsh", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
		`C:\scripts\display-scale.ps1`,
		"-DisplayName", "PG32UCDM", "-Query",
	}, args)
}
