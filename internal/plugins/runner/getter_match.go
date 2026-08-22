package runner

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
)

func mapGetterToOptionName(action ActionConfig, options map[string]OptionConfig, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("getter value is empty")
	}

	optionList := optionNamesFromMap(options)
	for _, opt := range optionList {
		if opt.Name == raw || opt.Value == raw {
			return opt.Name, nil
		}
	}

	keys := sortedOptionKeys(options)
	for _, key := range keys {
		opt := options[key]
		if getterRawMatchesOption(raw, opt.Parameters) {
			return entity.OptionName(optionList, key), nil
		}
	}
	return "", fmt.Errorf("getter value %q does not match any configured option", raw)
}

func sortedOptionKeys(options map[string]OptionConfig) []string {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func getterRawMatchesOption(raw string, params map[string]any) bool {
	width, hasWidth := paramValue(params, "Width")
	height, hasHeight := paramValue(params, "Height")
	if hasWidth && hasHeight && width != nil && height != nil {
		expected := fmt.Sprintf("%sx%s", formatScalar(width), formatScalar(height))
		if strings.EqualFold(strings.ReplaceAll(raw, "×", "x"), expected) {
			return true
		}
	}
	if scale, ok := paramValue(params, "Scale"); ok && scale != nil {
		scaleText := formatScalar(scale)
		if raw == scaleText || raw == scaleText+"%" || strings.EqualFold(raw, scaleText+"%") {
			return true
		}
	}
	if refresh, ok := paramValue(params, "RefreshRate"); ok && refresh != nil {
		if refreshMatches(raw, formatScalar(refresh)) {
			return true
		}
	}
	return false
}

func paramValue(params map[string]any, name string) (any, bool) {
	if params == nil {
		return nil, false
	}
	needle := strings.ToLower(name)
	for key, value := range params {
		if strings.ToLower(key) == needle {
			return value, true
		}
	}
	return nil, false
}

func refreshMatches(raw, configured string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	configured = strings.TrimSpace(strings.ToLower(configured))
	if raw == configured {
		return true
	}
	rawNum, err1 := strconv.ParseFloat(strings.ReplaceAll(raw, "_", "."), 64)
	cfgNum, err2 := strconv.ParseFloat(strings.ReplaceAll(configured, "_", "."), 64)
	if err1 == nil && err2 == nil {
		if math.Abs(rawNum-cfgNum) < 0.05 {
			return true
		}
		if math.Round(rawNum) == math.Round(cfgNum) {
			return true
		}
	}
	return false
}

func formatScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}
