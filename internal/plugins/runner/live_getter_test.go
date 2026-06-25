//go:build windows

package runner

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLiveDeskptyDisplayGetters(t *testing.T) {
	if os.Getenv("HOMEPPC_LIVE_GETTERS") != "1" {
		t.Skip("set HOMEPPC_LIVE_GETTERS=1 to run live deskpty getter integration")
	}

	deskpty := os.Getenv("HOMEPC_DESKPTY")
	if deskpty == "" {
		t.Skip("set HOMEPC_DESKPTY to deskpty.exe path")
	}
	if _, err := os.Stat(deskpty); err != nil {
		t.Skip("deskpty not installed")
	}

	deskptyUser := os.Getenv("HOMEPC_DESKPTY_USER")
	deskptyCWD := os.Getenv("HOMEPC_DESKPTY_CWD")
	if deskptyUser == "" || deskptyCWD == "" {
		t.Skip("set HOMEPC_DESKPTY_USER and HOMEPC_DESKPTY_CWD")
	}

	cfg := Config{
		DefaultCommand: deskpty,
		DefaultArgs: []string{
			"--user", deskptyUser,
			"--cwd", deskptyCWD,
			"--stdio", "--",
			"pwsh", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File",
		},
	}
	p := &Plugin{}

	cases := []struct {
		name   string
		action ActionConfig
	}{
		{
			name: "pg32ucdm_resolution",
			action: ActionConfig{
				Kind: kindSelect,
				Getter: GetterConfig{
					Path:       `C:\ProgramData\home-pc-agent\scripts\display-resolution.ps1`,
					StaticArgs: []string{"-DisplayName", "PG32UCDM", "-Query"},
				}.withPluginDefaults(cfg),
				Options: map[string]OptionConfig{
					"res_4k": {Name: "3840x2160", Parameters: map[string]any{"Width": 3840, "Height": 2160}},
				},
			},
		},
		{
			name: "pg32ucdm_scale",
			action: ActionConfig{
				Kind: kindSelect,
				Getter: GetterConfig{
					Path:       `C:\ProgramData\home-pc-agent\scripts\display-scale.ps1`,
					StaticArgs: []string{"-DisplayName", "PG32UCDM", "-Query"},
				}.withPluginDefaults(cfg),
				Options: map[string]OptionConfig{
					"scale_100": {Name: "100%", Parameters: map[string]any{"Scale": "100"}},
				},
			},
		},
		{
			name: "pg32ucdm_refresh",
			action: ActionConfig{
				Kind: kindSelect,
				Getter: GetterConfig{
					Path:       `C:\ProgramData\home-pc-agent\scripts\display-refresh.ps1`,
					StaticArgs: []string{"-DisplayName", "PG32UCDM", "-Query"},
				}.withPluginDefaults(cfg),
				Options: map[string]OptionConfig{
					"refresh_240_016": {Name: "240.016 Hz", Parameters: map[string]any{"RefreshRate": "240_016"}},
				},
			},
		},
		{
			name: "pg32ucdm_power",
			action: ActionConfig{
				Kind: kindSwitch,
				Getter: GetterConfig{
					Path:       `C:\ProgramData\home-pc-agent\scripts\display-power.ps1`,
					StaticArgs: []string{"-DisplayName", "PG32UCDM", "-State", "query"},
				}.withPluginDefaults(cfg),
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &actionRuntime{action: tc.action}
			value, err := p.runGetter(ctx, runtime)
			require.NoError(t, err)
			t.Logf("value=%v", value)
			require.NotNil(t, value)
		})
	}
}
