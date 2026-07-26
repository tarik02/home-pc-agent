package catalog

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	"github.com/tarik02/home-pc-agent/internal/plugins/display_kscreen"
	"github.com/tarik02/home-pc-agent/internal/plugins/display_windows"
	"github.com/tarik02/home-pc-agent/internal/plugins/fancontrol"
	"github.com/tarik02/home-pc-agent/internal/plugins/inhibit_freedesktop"
	"github.com/tarik02/home-pc-agent/internal/plugins/powerplan"
	"github.com/tarik02/home-pc-agent/internal/plugins/powerprofile_powerprofilesctl"
	"github.com/tarik02/home-pc-agent/internal/plugins/runner"
	"github.com/tarik02/home-pc-agent/internal/plugins/session_loginctl"
	"github.com/tarik02/home-pc-agent/internal/plugins/session_windows"
)

type PluginValidator func(c *config.Config) []error

type Entry struct {
	ID       string
	OS       []string
	Entities []string
	Factory  plugin.PluginFactory
	Validate PluginValidator
}

type enabledConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

func Builtin() []Entry {
	return []Entry{
		{
			ID:       "session_windows",
			OS:       []string{"windows"},
			Entities: []string{"session.lock", "session.sleep"},
			Factory:  session_windows.NewFactory(),
			Validate: decodeEnabled("session_windows"),
		},
		{
			ID:       "session_loginctl",
			OS:       []string{"linux"},
			Entities: []string{"session.lock", "session.sleep"},
			Factory:  session_loginctl.NewFactory(),
			Validate: decodeEnabled("session_loginctl"),
		},
		{
			ID:       "display_windows",
			OS:       []string{"windows"},
			Entities: []string{"display.off"},
			Factory:  display_windows.NewFactory(),
			Validate: decodeEnabled("display_windows"),
		},
		{
			ID:       "display_kscreen",
			OS:       []string{"linux"},
			Entities: []string{"display.off"},
			Factory:  display_kscreen.NewFactory(),
			Validate: decodeEnabled("display_kscreen"),
		},
		{
			ID:       "inhibit_freedesktop",
			OS:       []string{"linux"},
			Entities: []string{"session.lock_inhibited", "display.dim_inhibited"},
			Factory:  inhibit_freedesktop.NewFactory(),
			Validate: decodeEnabled("inhibit_freedesktop"),
		},
		{
			ID:       "powerplan_windows",
			OS:       []string{"windows"},
			Entities: []string{"powerplan.mode"},
			Factory:  powerplan.NewFactory(),
			Validate: validatePowerPlanWindows,
		},
		{
			ID:       "powerprofile_powerprofilesctl",
			OS:       []string{"linux"},
			Entities: []string{"powerprofile.profile"},
			Factory:  powerprofile_powerprofilesctl.NewFactory(),
			Validate: validatePowerProfile,
		},
		{
			ID:       "fancontrol_windows",
			OS:       []string{"windows"},
			Entities: []string{"fancontrol.profile"},
			Factory:  fancontrol.NewFactory(),
			Validate: validateFanControlWindows,
		},
		{
			ID:       "runner",
			Entities: []string{},
			Factory:  runner.NewFactory(),
			Validate: validateRunner,
		},
	}
}

func Factories() []plugin.PluginFactory {
	entries := Builtin()
	factories := make([]plugin.PluginFactory, 0, len(entries))
	for _, entry := range entries {
		factories = append(factories, entry.Factory)
	}
	return factories
}

func Validate(c *config.Config) error {
	return errors.Join(ValidatePlugins(c)...)
}

func ValidatePlugins(c *config.Config) []error {
	entries := Builtin()
	byID := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	var errs []error
	entityOwners := map[string]string{}

	for id := range c.Plugins {
		if _, ok := byID[id]; !ok {
			errs = append(errs, fmt.Errorf("plugins.%s: unknown builtin plugin", id))
		}
	}

	for id := range c.Plugins {
		entry, ok := byID[id]
		if !ok || !c.PluginEnabled(id) {
			continue
		}
		if !SupportedOn(entry, runtime.GOOS) {
			errs = append(errs, fmt.Errorf("plugins.%s: plugin is not supported on %s", id, runtime.GOOS))
			continue
		}
		for _, entityID := range entry.Entities {
			if owner, ok := entityOwners[entityID]; ok {
				errs = append(errs, fmt.Errorf("plugins.%s: entity %q is already provided by plugin %q", id, entityID, owner))
				continue
			}
			entityOwners[entityID] = id
		}
	}

	for _, entry := range entries {
		if entry.Validate == nil {
			continue
		}
		errs = append(errs, entry.Validate(c)...)
	}
	return errs
}

func SupportedOn(entry Entry, goos string) bool {
	if len(entry.OS) == 0 {
		return true
	}
	for _, os := range entry.OS {
		if os == goos {
			return true
		}
	}
	return false
}

func decodeEnabled(id string) PluginValidator {
	return func(c *config.Config) []error {
		if c.PluginRaw(id) == nil {
			return nil
		}
		var cfg enabledConfig
		if err := c.DecodePluginSection(id, &cfg); err != nil {
			return []error{fmt.Errorf("plugins.%s: %w", id, err)}
		}
		return nil
	}
}

func validatePowerProfile(c *config.Config) []error {
	if c.PluginRaw("powerprofile_powerprofilesctl") == nil {
		return nil
	}
	var cfg powerprofile_powerprofilesctl.Config
	if err := c.DecodePluginSection("powerprofile_powerprofilesctl", &cfg); err != nil {
		return []error{fmt.Errorf("plugins.powerprofile_powerprofilesctl: %w", err)}
	}
	return nil
}

func validatePowerPlanWindows(c *config.Config) []error {
	var cfg powerplan.Config
	if err := c.DecodePluginSection("powerplan_windows", &cfg); err != nil {
		return []error{fmt.Errorf("plugins.powerplan_windows: %w", err)}
	}
	if !cfg.Enabled || !SupportedOn(Entry{OS: []string{"windows"}}, runtime.GOOS) {
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return []error{fmt.Errorf("plugins.powerplan_windows: %w", err)}
	}
	return nil
}

func validateFanControlWindows(c *config.Config) []error {
	var cfg fancontrol.Config
	if err := c.DecodePluginSection("fancontrol_windows", &cfg); err != nil {
		return []error{fmt.Errorf("plugins.fancontrol_windows: %w", err)}
	}
	if !cfg.Enabled || !SupportedOn(Entry{OS: []string{"windows"}}, runtime.GOOS) {
		return nil
	}
	var errs []error
	for _, err := range fancontrol.ValidateConfigured(cfg) {
		errs = append(errs, fmt.Errorf("plugins.fancontrol_windows: %w", err))
	}
	return errs
}

func validateRunner(c *config.Config) []error {
	var cfg runner.Config
	if err := c.DecodePluginSection("runner", &cfg); err != nil {
		return []error{fmt.Errorf("plugins.runner: %w", err)}
	}
	if !c.PluginEnabled("runner") {
		return nil
	}
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return []error{fmt.Errorf("plugins.runner: %w", err)}
	}
	return nil
}
