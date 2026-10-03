package catalog

import (
	"errors"
	"fmt"
	"runtime"
	"sort"

	"go.uber.org/zap"

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

func Builtin() []plugin.Factory {
	return []plugin.Factory{
		session_windows.NewFactory(),
		session_loginctl.NewFactory(),
		display_windows.NewFactory(),
		display_kscreen.NewFactory(),
		inhibit_freedesktop.NewFactory(),
		powerplan.NewFactory(),
		powerprofile_powerprofilesctl.NewFactory(),
		fancontrol.NewFactory(),
		runner.NewFactory(),
	}
}

func Factories() []plugin.Factory {
	return Builtin()
}

func Validate(c *config.Config) error {
	return errors.Join(ValidatePlugins(c)...)
}

func ValidatePlugins(c *config.Config) []error {
	factories := Builtin()
	byID := make(map[string]plugin.Factory, len(factories))
	for _, factory := range factories {
		byID[factory.ID()] = factory
	}

	pluginIDs := make([]string, 0, len(c.Plugins))
	for id := range c.Plugins {
		pluginIDs = append(pluginIDs, id)
	}
	sort.Strings(pluginIDs)

	var errs []error
	entityOwners := make(map[string]string)
	for _, id := range pluginIDs {
		factory, ok := byID[id]
		if !ok {
			errs = append(errs, fmt.Errorf("plugins.%s: unknown builtin plugin", id))
			continue
		}

		raw := c.PluginRaw(id)
		if _, err := factory.New(raw, zap.NewNop()); err != nil {
			errs = append(errs, fmt.Errorf("plugins.%s: %w", id, err))
			continue
		}
		if !c.PluginEnabled(id) {
			continue
		}
		if !SupportedOn(factory.Descriptor, runtime.GOOS) {
			errs = append(errs, fmt.Errorf("plugins.%s: plugin is not supported on %s", id, runtime.GOOS))
			continue
		}
		if err := factory.Validate(raw); err != nil {
			errs = append(errs, fmt.Errorf("plugins.%s: %w", id, err))
			continue
		}

		entityIDs, err := factory.EntityIDs(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugins.%s: resolve entity IDs: %w", id, err))
			continue
		}
		for _, entityID := range entityIDs {
			if owner, ok := entityOwners[entityID]; ok {
				errs = append(errs, fmt.Errorf("plugins.%s: entity %q is already provided by plugin %q", id, entityID, owner))
				continue
			}
			entityOwners[entityID] = id
		}
	}
	return errs
}

func SupportedOn(descriptor plugin.Descriptor, goos string) bool {
	if len(descriptor.OperatingSystems) == 0 {
		return true
	}
	for _, operatingSystem := range descriptor.OperatingSystems {
		if operatingSystem == goos {
			return true
		}
	}
	return false
}
