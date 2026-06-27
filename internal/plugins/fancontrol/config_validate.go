package fancontrol

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var profileKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func ValidateConfigured(cfg Config) []error {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return []error{err}
	}

	var errs []error
	for field, path := range map[string]string{
		"exe_path":    cfg.ExePath,
		"config_path": cfg.ConfigPath,
	} {
		if strings.TrimSpace(path) == "" {
			errs = append(errs, fmt.Errorf("%s: required", field))
			continue
		}
		if err := ensureFileExists(path); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", field, err))
		}
	}

	for key, profile := range cfg.Profiles {
		if !profileKeyPattern.MatchString(key) {
			errs = append(errs, fmt.Errorf("profiles.%s: profile key must match %s", key, profileKeyPattern.String()))
		}
		if strings.TrimSpace(profile.SourcePath) != "" {
			if err := ensureFileExists(profile.SourcePath); err != nil {
				errs = append(errs, fmt.Errorf("profiles.%s.source_path: %w", key, err))
			}
		}
	}
	return errs
}

func ensureFileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, expected a file", path)
	}
	return nil
}
