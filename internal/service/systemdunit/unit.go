package systemdunit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

type Unit struct {
	Description string
	ExecStart   []string
}

func ValidateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf("service name %q must not contain %q", name, ".")
	}
	if !serviceNamePattern.MatchString(name) {
		return fmt.Errorf("service name %q must match %s", name, serviceNamePattern.String())
	}
	return nil
}

func ValidateUnitField(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return fmt.Errorf("%s contains control characters", field)
		}
	}
	return nil
}

func EscapeUnitValue(value string) (string, error) {
	if err := ValidateUnitField("unit value", value); err != nil {
		return "", err
	}
	return escapeSystemdValue(value), nil
}

func escapeSystemdValue(value string) string {
	needsQuotes := strings.ContainsAny(value, " \t\"\\%$")
	var b strings.Builder
	if needsQuotes {
		b.WriteByte('"')
	}
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\t':
			b.WriteString(`\t`)
		case '%':
			b.WriteString(`%%`)
		case '$':
			b.WriteString(`$$`)
		default:
			b.WriteRune(r)
		}
	}
	if needsQuotes {
		b.WriteByte('"')
	}
	return b.String()
}

func Render(unit Unit) (string, error) {
	if err := ValidateUnitField("Description", unit.Description); err != nil {
		return "", fmt.Errorf("description: %w", err)
	}
	if len(unit.ExecStart) == 0 {
		return "", fmt.Errorf("ExecStart requires at least one argument")
	}

	escaped := make([]string, 0, len(unit.ExecStart))
	for i, arg := range unit.ExecStart {
		value, err := EscapeUnitValue(arg)
		if err != nil {
			return "", fmt.Errorf("ExecStart argument %d: %w", i, err)
		}
		escaped = append(escaped, value)
	}

	description := escapeSystemdValue(unit.Description)

	return fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure

[Install]
WantedBy=default.target
`, description, strings.Join(escaped, " ")), nil
}
