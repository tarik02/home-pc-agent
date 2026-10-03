package session_loginctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	"github.com/tarik02/home-pc-agent/internal/externalcmd"
)

const (
	pluginID         = "session_loginctl"
	timeout          = 10 * time.Second
	lockPollInterval = 10 * time.Second
	lockStateID      = "session.locked"
)

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
	if err := externalcmd.RequireAll("loginctl", "systemctl"); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
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
	lockState := entity.Entity{
		ID:   lockStateID,
		Name: "Session Locked",
		Kind: entity.KindBinarySensor,
	}
	if _, err := host.RegisterEntity(lockState); err != nil {
		return err
	}
	p.refreshLockState(ctx)
	host.Go("session-lock-state", func(loopCtx context.Context) error {
		ticker := time.NewTicker(lockPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return nil
			case <-ticker.C:
				p.refreshLockState(loopCtx)
			}
		}
	})
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

func (p *Plugin) refreshLockState(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	locked, err := sessionLocked(queryCtx)
	if err != nil {
		_ = p.host.SetAvailability(lockStateID, entity.AvailabilityUnavailable)
		p.logger.Debug("session lock state unavailable", zap.Error(err))
		return
	}
	_ = p.host.SetAvailability(lockStateID, entity.AvailabilityOnline)
	_ = p.host.PublishState(lockStateID, locked)
}

func sessionLocked(ctx context.Context) (bool, error) {
	currentUser, err := user.Current()
	if err != nil {
		return false, fmt.Errorf("resolve current user: %w", err)
	}
	sessionID, err := commandOutput(ctx, "loginctl", "show-user", currentUser.Username, "--property=Display", "--value")
	if err != nil {
		return false, fmt.Errorf("resolve display session: %w", err)
	}
	if sessionID == "" {
		return false, fmt.Errorf("current user has no display session")
	}
	lockedHint, err := commandOutput(ctx, "loginctl", "show-session", sessionID, "--property=LockedHint", "--value")
	if err != nil {
		return false, fmt.Errorf("query session lock state: %w", err)
	}
	switch lockedHint {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected LockedHint value %q", lockedHint)
	}
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}
