package powerplan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeDiscoveredModesKeepsConfiguredOverrides(t *testing.T) {
	modes := map[string]Mode{
		"balanced": {
			Name: "Balanced",
			GUID: "381b4222-f694-41f0-9685-ff5bb260df2e",
		},
	}
	discovered := []Mode{
		{Name: "Збалансований", GUID: "381b4222-f694-41f0-9685-ff5bb260df2e"},
		{Name: "home-pc-agent-dev", GUID: "4966f2f5-8791-4126-9efc-86b5d6d79322"},
		{Name: "Висока продуктивність", GUID: "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"},
	}

	mergeDiscoveredModes(modes, discovered)

	require.Equal(t, "Balanced", modes["balanced"].Name)
	require.Equal(t, "home-pc-agent-dev", modes["home_pc_agent_dev"].Name)
	require.Equal(t, "Висока продуктивність", modes["plan_8c5e7fda"].Name)
}
