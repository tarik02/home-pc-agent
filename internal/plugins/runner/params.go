package runner

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func sortedParamKeys(params map[string]any) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneParameters(params map[string]any) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = value
	}
	return out
}

func validateStructuredParameters(schema map[string]ParameterSchema, params map[string]any, prefix string) error {
	if len(schema) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	for key, value := range params {
		fieldSchema, ok := schema[key]
		if !ok {
			return fmt.Errorf("%s: unknown parameter %q", prefix, key)
		}
		if err := validateParameterValue(key, fieldSchema, value); err != nil {
			return fmt.Errorf("%s.%s: %w", prefix, key, err)
		}
		seen[key] = struct{}{}
	}
	for key, fieldSchema := range schema {
		if fieldSchema.Required {
			if _, ok := params[key]; !ok {
				return fmt.Errorf("%s.%s: required", prefix, key)
			}
		}
	}
	return nil
}

func validateJSONParameters(schema map[string]ParameterSchema, params map[string]any, jsonCfg JSONConfig, raw []byte, prefix string) error {
	if len(raw) > jsonCfg.MaxBytes {
		return fmt.Errorf("%s: json payload exceeds max_bytes (%d > %d)", prefix, len(raw), jsonCfg.MaxBytes)
	}
	if len(schema) == 0 {
		return nil
	}
	if jsonCfg.Strict {
		return validateStructuredParameters(schema, params, prefix)
	}
	for key, value := range params {
		fieldSchema, ok := schema[key]
		if !ok {
			continue
		}
		if err := validateParameterValue(key, fieldSchema, value); err != nil {
			return fmt.Errorf("%s.%s: %w", prefix, key, err)
		}
	}
	for key, fieldSchema := range schema {
		if fieldSchema.Required {
			if _, ok := params[key]; !ok {
				return fmt.Errorf("%s.%s: required", prefix, key)
			}
		}
	}
	return nil
}

func validateParameterValue(name string, schema ParameterSchema, value any) error {
	if !paramNamePattern.MatchString(name) {
		return fmt.Errorf("parameter name %q is invalid", name)
	}
	switch schema.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		if schema.Pattern != "" {
			matched, err := regexp.MatchString(schema.Pattern, text)
			if err != nil {
				return err
			}
			if !matched {
				return fmt.Errorf("value %q does not match pattern", text)
			}
		}
		if len(schema.Enum) > 0 && !stringIn(schema.Enum, text) {
			return fmt.Errorf("value %q is not in enum", text)
		}
	case "boolean":
		switch value.(type) {
		case bool:
		case string:
			if _, err := normalizeSwitchState(value); err != nil {
				return fmt.Errorf("expected boolean")
			}
		default:
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		number, err := asInt64(value)
		if err != nil {
			return fmt.Errorf("expected integer")
		}
		if schema.Min != nil && number < int64(*schema.Min) {
			return fmt.Errorf("value below min %d", *schema.Min)
		}
		if schema.Max != nil && number > int64(*schema.Max) {
			return fmt.Errorf("value above max %d", *schema.Max)
		}
	case "number":
		number, err := asFloat64(value)
		if err != nil {
			return fmt.Errorf("expected number")
		}
		if schema.Min != nil && number < float64(*schema.Min) {
			return fmt.Errorf("value below min %d", *schema.Min)
		}
		if schema.Max != nil && number > float64(*schema.Max) {
			return fmt.Errorf("value above max %d", *schema.Max)
		}
	default:
		return fmt.Errorf("unsupported schema type %q", schema.Type)
	}
	return nil
}

func asInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.Trunc(typed) != typed || typed < math.MinInt64 || typed > math.MaxInt64 {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(typed), nil
	case json.Number:
		return typed.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func asFloat64(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func stringIn(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func payloadAsMap(payload any) (map[string]any, error) {
	switch typed := payload.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return cloneParameters(typed), nil
	default:
		return nil, fmt.Errorf("expected JSON object payload")
	}
}

func decodePayloadMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}
