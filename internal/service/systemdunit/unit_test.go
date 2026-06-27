package systemdunit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateServiceName(t *testing.T) {
	require.NoError(t, ValidateServiceName("home-pc-agent"))
	require.Error(t, ValidateServiceName(""))
	require.Error(t, ValidateServiceName("home.pc.agent"))
	require.Error(t, ValidateServiceName("-bad"))
}

func TestEscapeUnitValuePlainPath(t *testing.T) {
	value, err := EscapeUnitValue("/usr/bin/home-pc-agent")
	require.NoError(t, err)
	require.Equal(t, "/usr/bin/home-pc-agent", value)
}

func TestEscapeUnitValueSpacesQuotesBackslashes(t *testing.T) {
	value, err := EscapeUnitValue(`/etc/home pc/agent "quoted".toml`)
	require.NoError(t, err)
	require.Equal(t, `"/etc/home pc/agent \"quoted\".toml"`, value)

	value, err = EscapeUnitValue(`C:\Program Files\home-pc-agent\bin\agent.exe`)
	require.NoError(t, err)
	require.Equal(t, `"C:\\Program Files\\home-pc-agent\\bin\\agent.exe"`, value)
}

func TestEscapeUnitValuePercentAndDollar(t *testing.T) {
	value, err := EscapeUnitValue("/etc/config%home/agent.toml")
	require.NoError(t, err)
	require.Equal(t, `"/etc/config%%home/agent.toml"`, value)

	value, err = EscapeUnitValue("/etc/$HOME/agent.toml")
	require.NoError(t, err)
	require.Equal(t, `"/etc/$$HOME/agent.toml"`, value)

	value, err = EscapeUnitValue("/etc/%user/$HOME/config.toml")
	require.NoError(t, err)
	require.Equal(t, `"/etc/%%user/$$HOME/config.toml"`, value)
}

func TestEscapeUnitValueRejectsControlCharacters(t *testing.T) {
	_, err := EscapeUnitValue("bad\nvalue")
	require.Error(t, err)

	_, err = EscapeUnitValue("bad\rvalue")
	require.Error(t, err)
}

func TestRenderUnit(t *testing.T) {
	content, err := Render(Unit{
		Description: "home-pc-agent Agent",
		ExecStart: []string{
			"/usr/bin/home-pc-agent",
			"run",
			"--config",
			"/etc/home-pc-agent/config.toml",
		},
	})
	require.NoError(t, err)
	require.Contains(t, content, `Description="home-pc-agent Agent"`)
	require.Contains(t, content, "ExecStart=/usr/bin/home-pc-agent run --config /etc/home-pc-agent/config.toml")
}

func TestRenderUnitEscapesSpaces(t *testing.T) {
	content, err := Render(Unit{
		Description: "Agent Service",
		ExecStart: []string{
			"/usr/bin/home-pc-agent",
			"run",
			"--config",
			`/home/user/My Configs/home-pc-agent.toml`,
		},
	})
	require.NoError(t, err)
	require.True(t, strings.Contains(content, `"/home/user/My Configs/home-pc-agent.toml"`))
}

func TestRenderUnitEscapesPercentAndDollarInExecStart(t *testing.T) {
	content, err := Render(Unit{
		Description: "Agent Service",
		ExecStart: []string{
			"/usr/bin/home-pc-agent",
			"run",
			"--config",
			"/etc/%user/$HOME/config.toml",
		},
	})
	require.NoError(t, err)
	require.Contains(t, content, `ExecStart=/usr/bin/home-pc-agent run --config "/etc/%%user/$$HOME/config.toml"`)
}
