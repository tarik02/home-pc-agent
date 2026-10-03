package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapGetterToOptionNameRefresh240With240Param(t *testing.T) {
	action := ActionConfig{Kind: kindSelect}
	options := map[string]OptionConfig{
		"refresh_240": {
			Name:       "240 Hz",
			Parameters: map[string]any{"RefreshRate": "240"},
		},
	}

	name, err := mapGetterToOptionName(action, options, "240.016")
	require.NoError(t, err)
	require.Equal(t, "240 Hz", name)
}
