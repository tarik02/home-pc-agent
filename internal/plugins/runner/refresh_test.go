package runner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultGetterRefreshApplied(t *testing.T) {
	cfg := Config{
		DefaultGetterRefresh: 60 * time.Second,
		Actions: map[string]ActionConfig{
			"power": {
				State: StateConfig{Source: stateGetter},
			},
		},
	}
	cfg = cfg.WithDefaults()
	require.Equal(t, 60*time.Second, cfg.Actions["power"].Getter.Refresh)
}

func TestActionMatchesTagSet(t *testing.T) {
	tagSet := normalizedTagSet([]string{"display", "pg32ucdm"})
	require.True(t, actionMatchesTagSet([]string{"display", "vdd"}, tagSet))
	require.True(t, actionMatchesTagSet([]string{"pg32ucdm"}, tagSet))
	require.False(t, actionMatchesTagSet([]string{"vdd"}, tagSet))
}

func TestEffectiveRefreshTags(t *testing.T) {
	action := ActionConfig{
		Tags:        []string{"display"},
		RefreshTags: []string{"pg32ucdm"},
	}
	require.Equal(t, []string{"pg32ucdm"}, action.effectiveRefreshTags())

	action.RefreshTags = nil
	require.Equal(t, []string{"display"}, action.effectiveRefreshTags())
}
