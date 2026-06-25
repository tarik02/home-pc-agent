//go:build windows

package session_windows

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	win "github.com/tarik02/home-pc-agent/internal/windows"
)

const pluginID = "session_windows"

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
	return &Plugin{cfg: cfg, logger: ctx.Logger()}, nil
}

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

type Plugin struct {
	cfg    Config
	host   plugin.PluginHost
	logger *zap.Logger
}

func (p *Plugin) ID() string {
	return pluginID
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	p.host = host
	buttons := []entity.Entity{
		{ID: "session.lock", Name: "Lock PC", Kind: entity.KindButton, Icon: "mdi:lock"},
		{ID: "session.sleep", Name: "Sleep PC", Kind: entity.KindButton, Icon: "mdi:power-sleep"},
	}
	for _, button := range buttons {
		if _, err := host.RegisterEntity(button); err != nil {
			return err
		}
		id := button.ID
		if err := host.SubscribeCommand(id, func(ctx context.Context, command plugin.Command) error {
			return p.handleButton(ctx, id)
		}); err != nil {
			return err
		}
		_ = host.SetAvailability(button.ID, entity.AvailabilityOnline)
	}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) handleButton(ctx context.Context, id string) error {
	switch id {
	case "session.lock":
		return win.LockWorkStation()
	case "session.sleep":
		return win.Sleep()
	default:
		return fmt.Errorf("unknown session button %q", id)
	}
}
