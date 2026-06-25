//go:build windows

package powerplan

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverPowerPlansLive(t *testing.T) {
	if os.Getenv("HOMEPC_POWERPLAN_DISCOVERY_LIVE") == "" {
		t.Skip("set HOMEPC_POWERPLAN_DISCOVERY_LIVE=1 to query Windows power schemes")
	}

	modes, err := discoverPowerPlans(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, modes)

	names := make([]string, 0, len(modes))
	for _, mode := range modes {
		require.NotEmpty(t, mode.Name)
		require.NotEmpty(t, mode.GUID)
		names = append(names, mode.Name)
	}
	t.Logf("discovered power plans: %v", names)
	require.Contains(t, names, "home-pc-agent-dev")
}
