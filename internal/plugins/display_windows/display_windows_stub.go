//go:build !windows

package display_windows

import (
	"context"
	"fmt"

	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const pluginID = "display_windows"

type Factory struct{}

func NewFactory() Factory {
	return Factory{}
}

func (Factory) ID() string {
	return pluginID
}

func (Factory) New(ctx plugin.PluginFactoryContext) (plugin.Plugin, error) {
	var cfg Config
	if err := ctx.DecodeConfig(&cfg); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

type Plugin struct {
	cfg Config
}

func (p *Plugin) ID() string {
	return pluginID
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	return fmt.Errorf("display_windows is only supported on Windows")
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}
