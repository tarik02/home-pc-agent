package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOptionsGetterJSON(t *testing.T) {
	opts, err := parseOptionsGetterJSON(`[
		{"name":"3840x2160","parameters":{"DisplayName":"PG32UCDM","Width":3840,"Height":2160}},
		{"name":"1920x1080","parameters":{"DisplayName":"PG32UCDM","Width":1920,"Height":1080}}
	]`)
	require.NoError(t, err)
	require.Len(t, opts, 2)
	require.Equal(t, "3840x2160", opts["3840x2160"].Name)
	require.Equal(t, "PG32UCDM", opts["3840x2160"].Parameters["DisplayName"])
}

func TestParseOptionsGetterJSONDedupesNames(t *testing.T) {
	opts, err := parseOptionsGetterJSON(`[
		{"name":"240 Hz","parameters":{"RefreshRate":"240"}},
		{"name":"240 Hz","parameters":{"RefreshRate":"240_016"}}
	]`)
	require.NoError(t, err)
	require.Len(t, opts, 1)
}

func TestParseOptionsGetterJSONSingleObject(t *testing.T) {
	opts, err := parseOptionsGetterJSON(`{"name":"3840x2160","parameters":{"Width":3840,"Height":2160}}`)
	require.NoError(t, err)
	require.Len(t, opts, 1)
}

func TestSlugOptionKey(t *testing.T) {
	require.Equal(t, "240_hz", slugOptionKey("240 Hz", 0))
}
