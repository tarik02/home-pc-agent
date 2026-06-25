package mqtt

import (
	"testing"

	"github.com/tarik02/home-pc-agent/internal/core/entity"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryPayloadForSelect(t *testing.T) {
	e := entity.Entity{
		ID:   "powerplan.mode",
		Name: "Power Plan",
		Kind: entity.KindSelect,
		Options: []entity.Option{
			{Value: "dev", Name: "Development"},
			{Value: "gaming", Name: "Gaming"},
		},
		Icon: "mdi:power-plug",
	}
	opts := DiscoveryOptions{
		AgentID:       "desktop-pc",
		AgentName:     "Desktop PC",
		TopicPrefix:   "pc/desktop",
		DiscoveryBase: "homeassistant",
	}

	require.Equal(t, "homeassistant/select/pc_desktop/desktop_pc_powerplan_mode/config", DiscoveryTopic(e, opts))
	payload := DiscoveryPayload(e, opts)
	require.Equal(t, "Power Plan", payload["name"])
	require.Equal(t, "pc/desktop/state/powerplan.mode", payload["state_topic"])
	require.Equal(t, "pc/desktop/command/powerplan.mode", payload["command_topic"])
	require.Equal(t, []string{"Development", "Gaming"}, payload["options"])
	require.Equal(t, `{"value":"{{ value }}"}`, payload["command_template"])
	require.NotContains(t, payload, "value_template")
}

func TestDiscoveryTopicUsesTopicPrefixAsNodeID(t *testing.T) {
	e := entity.Entity{ID: "powerplan.mode", Name: "Power Plan", Kind: entity.KindSelect, Options: []entity.Option{{Value: "balanced", Name: "Balanced"}}}
	opts := DiscoveryOptions{AgentID: "desktop-pc", AgentName: "Desktop PC", TopicPrefix: "pc"}

	require.Equal(t, "homeassistant/select/pc/desktop_pc_powerplan_mode/config", DiscoveryTopic(e, opts))
}

func TestDiscoveryPayloadForButton(t *testing.T) {
	e := entity.Entity{ID: "session.lock", Name: "Lock PC", Kind: entity.KindButton}
	opts := DiscoveryOptions{AgentID: "desktop-pc", AgentName: "Desktop PC", TopicPrefix: "pc/desktop"}

	payload := DiscoveryPayload(e, opts)
	require.Equal(t, "pc/desktop/command/session.lock", payload["command_topic"])
	require.Equal(t, `{"action":"press"}`, payload["payload_press"])
	require.NotContains(t, payload, "state_topic")
}

func TestDecodeCommandPayload(t *testing.T) {
	payload, err := decodeCommandPayload([]byte(`{"value":"Gaming"}`))
	require.NoError(t, err)
	require.Equal(t, "Gaming", payload)

	payload, err = decodeCommandPayload([]byte(`{"action":"press"}`))
	require.NoError(t, err)
	require.Equal(t, "press", payload)
}

func TestStatePlainTextUsesEnvelopeState(t *testing.T) {
	envelope := map[string]any{
		"state":     "unknown",
		"exit_code": 0,
		"stdout":    "ok",
	}
	require.Equal(t, "unknown", statePlainText(envelope))

	require.Equal(t, "gaming", statePlainText("gaming"))
	require.Equal(t, "true", statePlainText(true))
	require.Equal(t, "", statePlainText(nil))
}
