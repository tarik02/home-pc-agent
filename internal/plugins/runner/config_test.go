package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateSelectAction(t *testing.T) {
	script := writeTempScript(t, `exit 0`)

	cfg := Config{
		Enabled:        true,
		DefaultTimeout: time.Second,
		Actions: map[string]ActionConfig{
			"display_mode": {
				Name: "Display Mode",
				Kind: kindSelect,
				State: StateConfig{
					Source: stateNone,
				},
				Setter: SetterConfig{
					Path:        script,
					Interpreter: interpreterPwsh,
					Input:       inputStructured,
					Delivery:    deliveryArgs,
				},
				Options: map[string]OptionConfig{
					"default": {
						Name:       "Default",
						Parameters: map[string]any{"Mode": "default"},
					},
				},
			},
		},
	}
	cfg = cfg.WithDefaults()
	require.NoError(t, cfg.Validate())
	require.Equal(t, "config", cfg.Actions["display_mode"].entityCategory())
}

func TestConfigRequiresCommandExecutable(t *testing.T) {
	script := writeTempScript(t, `exit 0`)

	cfg := Config{
		Enabled:        true,
		DefaultTimeout: time.Second,
		DefaultCommand: filepath.Join(t.TempDir(), "missing.exe"),
		Actions: map[string]ActionConfig{
			"toggle": {
				Name: "Toggle",
				Kind: kindSwitch,
				State: StateConfig{
					Source: stateGetter,
				},
				Setter: SetterConfig{
					Path: script,
					On: SideConfig{
						Parameters: map[string]any{"State": "on"},
					},
					Off: SideConfig{
						Parameters: map[string]any{"State": "off"},
					},
				},
				Getter: GetterConfig{
					Path: script,
				},
			},
		},
	}
	cfg = cfg.WithDefaults()
	require.Error(t, cfg.Validate())
}

func writeTempScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "script.ps1")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
