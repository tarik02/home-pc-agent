//go:build !darwin

package display_darwin

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const pluginID = "display_darwin"

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{ID: pluginID, OperatingSystems: []string{"darwin"}, EntityIDs: []string{"display.off"}},
		nil,
		func(cfg Config, logger *zap.Logger) plugin.Plugin {
			return &Plugin{cfg: cfg}
		},
	)
}

type Plugin struct {
	cfg Config
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	return fmt.Errorf("display_darwin is only supported on macOS")
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}
