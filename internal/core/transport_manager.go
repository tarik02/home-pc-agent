package core

import (
	"context"
	"fmt"
	"sort"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	coretransport "github.com/tarik02/home-pc-agent/internal/core/transport"

	"go.uber.org/zap"
)

type TransportManager struct {
	cfg        *config.Config
	registry   *EntityRegistry
	router     *CommandRouter
	bus        *events.EventBus
	logger     *zap.Logger
	transports map[string]coretransport.Transport
}

func NewTransportManager(cfg *config.Config, registry *EntityRegistry, router *CommandRouter, bus *events.EventBus, logger *zap.Logger, transports []coretransport.Transport) *TransportManager {
	transportMap := make(map[string]coretransport.Transport, len(transports))
	for _, t := range transports {
		transportMap[t.ID()] = t
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TransportManager{
		cfg:        cfg,
		registry:   registry,
		router:     router,
		bus:        bus,
		logger:     logger,
		transports: transportMap,
	}
}

func (m *TransportManager) Start(ctx context.Context) error {
	ids := make([]string, 0, len(m.transports))
	for id := range m.transports {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	host := &transportHost{
		cfg:      m.cfg,
		registry: m.registry,
		router:   m.router,
		bus:      m.bus,
		logger:   m.logger,
	}
	for _, id := range ids {
		t := m.transports[id]
		if err := t.Start(ctx, host); err != nil {
			return fmt.Errorf("start transport %q: %w", id, err)
		}
		m.logger.Info("transport started", zap.String("transport", id))
	}
	return nil
}

func (m *TransportManager) Reload(ctx context.Context, cfg *config.Config, transports []coretransport.Transport) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	m.cfg = cfg
	m.transports = make(map[string]coretransport.Transport, len(transports))
	for _, t := range transports {
		m.transports[t.ID()] = t
	}
	return m.Start(ctx)
}

func (m *TransportManager) Stop(ctx context.Context) error {
	ids := make([]string, 0, len(m.transports))
	for id := range m.transports {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	var firstErr error
	for _, id := range ids {
		t := m.transports[id]
		if err := t.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop transport %q: %w", id, err)
		}
		m.logger.Info("transport stopped", zap.String("transport", id))
	}
	return firstErr
}

type transportHost struct {
	cfg      *config.Config
	registry *EntityRegistry
	router   *CommandRouter
	bus      *events.EventBus
	logger   *zap.Logger
}

func (h *transportHost) AgentID() string {
	return h.cfg.Agent.ID
}

func (h *transportHost) AgentName() string {
	return h.cfg.Agent.Name
}

func (h *transportHost) Entities() []entity.Entity {
	return h.registry.List()
}

func (h *transportHost) Logger() *zap.Logger {
	return h.logger
}

func (h *transportHost) RouteCommand(ctx context.Context, command plugin.Command) error {
	return h.router.Route(ctx, command)
}

func (h *transportHost) SubscribeEvents(ctx context.Context, buffer int) <-chan events.Event {
	return h.bus.Subscribe(ctx, buffer)
}
