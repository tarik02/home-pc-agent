//go:build !windows

package session_windows

import (
	"context"
	"fmt"

	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	"go.uber.org/zap"
)

const pluginID = "session_windows"

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{
			ID:               pluginID,
			OperatingSystems: []string{"windows"},
			EntityIDs:        []string{"session.lock", "session.sleep"},
		},
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
	return fmt.Errorf("session_windows is only supported on Windows")
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}
