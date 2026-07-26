package inhibit_freedesktop

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const pluginID = "inhibit_freedesktop"

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

type inhibitor struct {
	entity        entity.Entity
	destination   string
	path          dbus.ObjectPath
	interfaceName string
	cookie        uint32
	active        bool
}

type Plugin struct {
	cfg        Config
	host       plugin.PluginHost
	logger     *zap.Logger
	conn       *dbus.Conn
	inhibitors map[string]*inhibitor
	mu         sync.Mutex
}

func (p *Plugin) ID() string {
	return pluginID
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect to session bus: %w", err)
	}
	started := false
	defer func() {
		if !started {
			_ = conn.Close()
		}
	}()

	inhibitors := []*inhibitor{
		{
			entity:        entity.Entity{ID: "session.lock_inhibited", Name: "Lock Inhibited", Kind: entity.KindSwitch, Icon: "mdi:lock-clock"},
			destination:   "org.freedesktop.ScreenSaver",
			path:          dbus.ObjectPath("/ScreenSaver"),
			interfaceName: "org.freedesktop.ScreenSaver",
		},
		{
			entity:        entity.Entity{ID: "display.dim_inhibited", Name: "Screen Dim Inhibited", Kind: entity.KindSwitch, Icon: "mdi:brightness-6"},
			destination:   "org.freedesktop.PowerManagement.Inhibit",
			path:          dbus.ObjectPath("/org/freedesktop/PowerManagement/Inhibit"),
			interfaceName: "org.freedesktop.PowerManagement.Inhibit",
		},
	}

	for _, item := range inhibitors {
		if call := conn.Object(item.destination, item.path).CallWithContext(ctx, "org.freedesktop.DBus.Peer.Ping", 0); call.Err != nil {
			return fmt.Errorf("connect to %s: %w", item.destination, call.Err)
		}
	}

	p.host = host
	p.conn = conn
	p.inhibitors = make(map[string]*inhibitor, len(inhibitors))
	for _, item := range inhibitors {
		p.inhibitors[item.entity.ID] = item
		if _, err := host.RegisterEntity(item.entity); err != nil {
			return err
		}
		id := item.entity.ID
		if err := host.SubscribeCommand(id, func(ctx context.Context, command plugin.Command) error {
			return p.handleCommand(ctx, id, command)
		}); err != nil {
			return err
		}
		if err := host.PublishState(id, false); err != nil {
			return err
		}
		if err := host.SetAvailability(id, entity.AvailabilityOnline); err != nil {
			return err
		}
	}

	started = true
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var stopErr error
	for _, item := range p.inhibitors {
		if !item.active {
			continue
		}
		if call := p.conn.Object(item.destination, item.path).CallWithContext(ctx, item.interfaceName+".UnInhibit", 0, item.cookie); call.Err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("release %s: %w", item.entity.ID, call.Err))
		}
		item.active = false
	}
	if err := p.conn.Close(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("close session bus: %w", err))
	}
	return stopErr
}

func (p *Plugin) handleCommand(ctx context.Context, id string, command plugin.Command) error {
	enabled, ok := command.Payload.(bool)
	if !ok {
		return fmt.Errorf("%s command payload must be a boolean", id)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	item := p.inhibitors[id]
	if item.active == enabled {
		return p.host.PublishState(id, enabled)
	}

	object := p.conn.Object(item.destination, item.path)
	if enabled {
		var cookie uint32
		if call := object.CallWithContext(ctx, item.interfaceName+".Inhibit", 0, "home-pc-agent", "Controlled by Home Assistant"); call.Err != nil {
			_ = p.host.SetAvailability(id, entity.AvailabilityUnavailable)
			return fmt.Errorf("enable %s: %w", id, call.Err)
		} else if err := call.Store(&cookie); err != nil {
			_ = p.host.SetAvailability(id, entity.AvailabilityUnavailable)
			return fmt.Errorf("read %s inhibitor cookie: %w", id, err)
		}
		item.cookie = cookie
		item.active = true
	} else {
		if call := object.CallWithContext(ctx, item.interfaceName+".UnInhibit", 0, item.cookie); call.Err != nil {
			_ = p.host.SetAvailability(id, entity.AvailabilityUnavailable)
			return fmt.Errorf("disable %s: %w", id, call.Err)
		}
		item.active = false
	}

	_ = p.host.SetAvailability(id, entity.AvailabilityOnline)
	return p.host.PublishState(id, enabled)
}
