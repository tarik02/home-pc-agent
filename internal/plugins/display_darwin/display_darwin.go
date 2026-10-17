//go:build darwin

package display_darwin

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	"github.com/tarik02/home-pc-agent/internal/externalcmd"
	"github.com/tarik02/home-pc-agent/internal/macos"
)

const (
	pluginID = "display_darwin"
	entityID = "display.off"
	timeout  = 10 * time.Second
)

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{ID: pluginID, OperatingSystems: []string{"darwin"}, EntityIDs: []string{entityID}},
		nil,
		func(cfg Config, logger *zap.Logger) plugin.Plugin {
			return &Plugin{cfg: cfg, logger: logger}
		},
	)
}

type Plugin struct {
	cfg    Config
	host   plugin.PluginHost
	logger *zap.Logger
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	if err := externalcmd.Require("/usr/bin/pmset"); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	p.host = host
	button := entity.Entity{ID: entityID, Name: "Display Off", Kind: entity.KindButton, Icon: "mdi:monitor-off"}
	if err := host.RegisterEntity(button); err != nil {
		return err
	}
	if err := host.SubscribeCommand(entityID, func(ctx context.Context, command plugin.Command) error {
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return macos.DisplayOff(cmdCtx)
	}); err != nil {
		return err
	}
	return host.SetAvailability(entityID, entity.AvailabilityOnline)
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}
