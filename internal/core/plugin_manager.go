package core

import (
	"context"
	"fmt"
	"sort"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"

	"go.uber.org/zap"
)

type PluginManager struct {
	cfg       *config.Config
	registry  *EntityRegistry
	router    *CommandRouter
	bus       *events.EventBus
	logger    *zap.Logger
	factories map[string]plugin.PluginFactory
	runtimes  map[string]*pluginRuntime
}

type pluginRuntime struct {
	plugin plugin.Plugin
	scope  *PluginScope
}

func NewPluginManager(cfg *config.Config, registry *EntityRegistry, router *CommandRouter, bus *events.EventBus, logger *zap.Logger, factories []plugin.PluginFactory) *PluginManager {
	factoryMap := make(map[string]plugin.PluginFactory, len(factories))
	for _, factory := range factories {
		factoryMap[factory.ID()] = factory
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PluginManager{
		cfg:       cfg,
		registry:  registry,
		router:    router,
		bus:       bus,
		logger:    logger,
		factories: factoryMap,
		runtimes:  make(map[string]*pluginRuntime),
	}
}

func (m *PluginManager) Start(ctx context.Context) error {
	ids := make([]string, 0, len(m.factories))
	for id := range m.factories {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		if !m.cfg.PluginEnabled(id) {
			m.logger.Info("plugin disabled", zap.String("plugin", id))
			continue
		}
		if _, ok := m.runtimes[id]; ok {
			return fmt.Errorf("plugin %q is already running", id)
		}

		factory := m.factories[id]
		logger := m.logger.With(zap.String("plugin", id))
		factoryCtx := NewFactoryContext(m.cfg.PluginRaw(id), logger, m.cfg.Agent.ID, m.cfg.Agent.DataDir)
		instance, err := factory.New(factoryCtx)
		if err != nil {
			return fmt.Errorf("create plugin %q: %w", id, err)
		}

		scope := NewPluginScope(ctx, id, m.registry, m.router, m.bus, logger)
		if err := instance.Start(ctx, scope); err != nil {
			_ = scope.Close(context.Background())
			return fmt.Errorf("start plugin %q: %w", id, err)
		}
		m.runtimes[id] = &pluginRuntime{plugin: instance, scope: scope}
		logger.Info("plugin started")
	}
	return nil
}

func (m *PluginManager) Reload(ctx context.Context, cfg *config.Config) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	m.cfg = cfg
	return m.Start(ctx)
}

func (m *PluginManager) Stop(ctx context.Context) error {
	ids := make([]string, 0, len(m.runtimes))
	for id := range m.runtimes {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	var firstErr error
	for _, id := range ids {
		runtime := m.runtimes[id]
		logger := m.logger.With(zap.String("plugin", id))
		if err := runtime.plugin.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop plugin %q: %w", id, err)
		}
		if err := runtime.scope.Close(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("cleanup plugin %q: %w", id, err)
		}
		delete(m.runtimes, id)
		logger.Info("plugin stopped")
	}
	return firstErr
}
