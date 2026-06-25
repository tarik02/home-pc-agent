package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateJSONParametersStrict(t *testing.T) {
	schema := map[string]ParameterSchema{
		"profile": {Type: "string", Required: true},
	}
	params := map[string]any{
		"profile": "quiet",
		"extra":   "nope",
	}
	err := validateJSONParameters(schema, params, JSONConfig{MaxBytes: 128, Strict: true}, []byte(`{"profile":"quiet","extra":"nope"}`), "command")
	require.Error(t, err)
}

func TestValidateStructuredEnum(t *testing.T) {
	schema := map[string]ParameterSchema{
		"Mode": {
			Type:     "string",
			Required: true,
			Enum:     []string{"default", "gaming-chair"},
		},
	}
	err := validateStructuredParameters(schema, map[string]any{"Mode": "invalid"}, "command")
	require.Error(t, err)
	require.NoError(t, validateStructuredParameters(schema, map[string]any{"Mode": "default"}, "command"))
}
