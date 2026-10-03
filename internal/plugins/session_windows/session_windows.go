//go:build windows

package session_windows

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	win "github.com/tarik02/home-pc-agent/internal/windows"
)

const (
	pluginID         = "session_windows"
	lockPollInterval = 10 * time.Second
	lockStateID      = "session.locked"
)

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
	buttons := []entity.Entity{
		{ID: "session.lock", Name: "Lock PC", Kind: entity.KindButton, Icon: "mdi:lock"},
		{ID: "session.sleep", Name: "Sleep PC", Kind: entity.KindButton, Icon: "mdi:power-sleep"},
	}
	for _, button := range buttons {
		if err := host.RegisterEntity(button); err != nil {
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
	lockState := entity.Entity{
		ID:   lockStateID,
		Name: "Session Locked",
		Kind: entity.KindBinarySensor,
	}
	if err := host.RegisterEntity(lockState); err != nil {
		return err
	}
	p.refreshLockState()
	host.Go("session-lock-state", func(loopCtx context.Context) error {
		ticker := time.NewTicker(lockPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return nil
			case <-ticker.C:
				p.refreshLockState()
			}
		}
	})
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

func (p *Plugin) refreshLockState() {
	locked, err := win.WorkstationLocked()
	if err != nil {
		_ = p.host.SetAvailability(lockStateID, entity.AvailabilityUnavailable)
		p.logger.Debug("session lock state unavailable", zap.Error(err))
		return
	}
	_ = p.host.SetAvailability(lockStateID, entity.AvailabilityOnline)
	_ = p.host.PublishState(lockStateID, locked)
}
