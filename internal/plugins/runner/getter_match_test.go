package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParamValueIsCaseInsensitive(t *testing.T) {
	value, ok := paramValue(map[string]any{"scale": "100", "displayname": "PG32UCDM"}, "Scale")
	require.True(t, ok)
	require.Equal(t, "100", value)
}

func TestMapGetterToOptionNameResolution(t *testing.T) {
	p := &Plugin{}
	action := ActionConfig{Kind: kindSelect}
	options := map[string]OptionConfig{
		"res_4k": {
			Name:       "3840x2160",
			Parameters: map[string]any{"Width": 3840, "Height": 2160},
		},
	}

	name, err := p.mapGetterToOptionName(action, options, "3840x2160")
	require.NoError(t, err)
	require.Equal(t, "3840x2160", name)
}

func TestMapGetterToOptionNameScale(t *testing.T) {
	p := &Plugin{}
	action := ActionConfig{Kind: kindSelect}
	options := map[string]OptionConfig{
		"scale_175": {
			Name:       "175%",
			Parameters: map[string]any{"Scale": "175"},
		},
	}

	name, err := p.mapGetterToOptionName(action, options, "175")
	require.NoError(t, err)
	require.Equal(t, "175%", name)
}

func TestMapGetterToOptionNameScaleLowercaseParams(t *testing.T) {
	p := &Plugin{}
	action := ActionConfig{Kind: kindSelect}
	options := map[string]OptionConfig{
		"scale_100": {
			Name:       "100%",
			Parameters: map[string]any{"scale": "100", "displayname": "PG32UCDM"},
		},
	}

	name, err := p.mapGetterToOptionName(action, options, "100")
	require.NoError(t, err)
	require.Equal(t, "100%", name)
}

func TestMapGetterToOptionNameRefresh240016(t *testing.T) {
	p := &Plugin{}
	action := ActionConfig{Kind: kindSelect}
	options := map[string]OptionConfig{
		"refresh_240": {
			Name:       "240 Hz",
			Parameters: map[string]any{"refreshrate": "240_016"},
		},
	}

	name, err := p.mapGetterToOptionName(action, options, "240.016")
	require.NoError(t, err)
	require.Equal(t, "240 Hz", name)
}
