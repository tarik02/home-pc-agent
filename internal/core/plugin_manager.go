package core

import (
	"context"
	"errors"
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
	factories map[string]plugin.Factory
	runtimes  map[string]*pluginRuntime
}

type pluginRuntime struct {
	plugin plugin.Plugin
	scope  *PluginScope
}

func NewPluginManager(cfg *config.Config, registry *EntityRegistry, router *CommandRouter, bus *events.EventBus, logger *zap.Logger, factories []plugin.Factory) *PluginManager {
	factoryMap := make(map[string]plugin.Factory, len(factories))
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
		instance, err := factory.New(m.cfg.PluginRaw(id), logger)
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
			cleanupErr := m.Stop(cleanupCtx)
			cancel()
			return errors.Join(fmt.Errorf("create plugin %q: %w", id, err), cleanupErr)
		}

		scope := NewPluginScope(ctx, id, m.registry, m.router, m.bus, logger)
		if err := instance.Start(ctx, scope); err != nil {
			scopeCtx, scopeCancel := context.WithTimeout(context.Background(), defaultStopTimeout)
			scopeErr := scope.Close(scopeCtx)
			scopeCancel()
			managerCtx, managerCancel := context.WithTimeout(context.Background(), defaultStopTimeout)
			managerErr := m.Stop(managerCtx)
			managerCancel()
			return errors.Join(fmt.Errorf("start plugin %q: %w", id, err), scopeErr, managerErr)
		}
		m.runtimes[id] = &pluginRuntime{plugin: instance, scope: scope}
		logger.Info("plugin started")
	}
	return nil
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
