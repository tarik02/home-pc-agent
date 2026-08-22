//go:build windows

package display_windows

import (
	"context"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	win "github.com/tarik02/home-pc-agent/internal/windows"
)

const (
	pluginID = "display_windows"
	entityID = "display.off"
)

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{ID: pluginID, OperatingSystems: []string{"windows"}, EntityIDs: []string{entityID}},
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
	p.host = host
	button := entity.Entity{ID: entityID, Name: "Display Off", Kind: entity.KindButton, Icon: "mdi:monitor-off"}
	if err := host.RegisterEntity(button); err != nil {
		return err
	}
	if err := host.SubscribeCommand(entityID, func(ctx context.Context, command plugin.Command) error {
		return win.DisplayOff()
	}); err != nil {
		return err
	}
	return host.SetAvailability(entityID, entity.AvailabilityOnline)
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}
