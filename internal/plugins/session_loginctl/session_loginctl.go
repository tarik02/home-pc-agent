package session_loginctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	"github.com/tarik02/home-pc-agent/internal/externalcmd"
)

const (
	pluginID = "session_loginctl"
	timeout  = 10 * time.Second
)

type Config struct {
	Enabled bool `mapstructure:"enabled"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{
			ID:               pluginID,
			OperatingSystems: []string{"linux"},
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
	if err := externalcmd.RequireAll("loginctl", "systemctl"); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
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
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) handleButton(ctx context.Context, id string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch id {
	case "session.lock":
		cmd := exec.CommandContext(cmdCtx, "loginctl", "lock-session")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("lock session: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	case "session.sleep":
		cmd := exec.CommandContext(cmdCtx, "systemctl", "suspend")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("suspend: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	default:
		return fmt.Errorf("unknown session button %q", id)
	}
}
