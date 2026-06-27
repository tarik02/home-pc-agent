package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	coretransport "github.com/tarik02/home-pc-agent/internal/core/transport"

	"go.uber.org/zap"
)

type TransportBuilder func(cfg *config.Config, logger *zap.Logger) []coretransport.Transport

type ConfigValidator func(cfg *config.Config) error

type Runtime struct {
	cfgPath         string
	cfg             *config.Config
	bus             *events.EventBus
	registry        *EntityRegistry
	router          *CommandRouter
	plugins         *PluginManager
	transports      *TransportManager
	logger          *zap.Logger
	factories       []plugin.PluginFactory
	buildTransports TransportBuilder
	validateConfig  ConfigValidator
	configWatcher   *config.Watcher

	mu sync.Mutex
}

func NewRuntime(cfgPath string, cfg *config.Config, logger *zap.Logger, factories []plugin.PluginFactory, buildTransports TransportBuilder, transports []coretransport.Transport, validateConfig ConfigValidator) (*Runtime, error) {
	if validateConfig == nil {
		return nil, fmt.Errorf("validateConfig is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if buildTransports == nil {
		buildTransports = func(cfg *config.Config, logger *zap.Logger) []coretransport.Transport { return transports }
	}
	bus := events.NewBus()
	registry := NewEntityRegistry(bus)
	router := NewCommandRouter()
	return &Runtime{
		cfgPath:         cfgPath,
		cfg:             cfg,
		bus:             bus,
		registry:        registry,
		router:          router,
		plugins:         NewPluginManager(cfg, registry, router, bus, logger, factories),
		transports:      NewTransportManager(cfg, registry, router, bus, logger, transports),
		logger:          logger,
		factories:       factories,
		buildTransports: buildTransports,
		validateConfig:  validateConfig,
	}, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	watcher, err := config.Watch(r.cfgPath, func() {
		r.reloadFromDisk(ctx)
	})
	if err != nil {
		return fmt.Errorf("watch config %q: %w", r.cfgPath, err)
	}
	r.configWatcher = watcher
	defer r.configWatcher.Close()

	if err := r.transports.Start(ctx); err != nil {
		return err
	}
	if err := r.plugins.Start(ctx); err != nil {
		_ = r.transports.Stop(context.Background())
		return err
	}

	<-ctx.Done()
	r.logger.Info("stopping runtime")

	stopCtx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
	defer cancel()
	pluginErr := r.plugins.Stop(stopCtx)
	transportErr := r.transports.Stop(stopCtx)
	if pluginErr != nil {
		return pluginErr
	}
	return transportErr
}

func (r *Runtime) reloadFromDisk(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := config.Load(r.cfgPath)
	if err != nil {
		r.logger.Warn("config reload skipped: failed to read config", zap.String("path", r.cfgPath), zap.Error(err))
		return
	}
	if err := r.validateConfig(cfg); err != nil {
		r.logger.Warn("config reload skipped: validation failed", zap.String("path", r.cfgPath), zap.Error(err))
		return
	}

	stopCtx, cancel := context.WithTimeout(ctx, defaultStopTimeout)
	defer cancel()

	if err := r.plugins.Stop(stopCtx); err != nil {
		r.logger.Warn("config reload aborted: failed to stop plugins", zap.Error(err))
		return
	}

	r.cfg = cfg
	transports := r.buildTransports(cfg, r.logger)
	if err := r.transports.Reload(ctx, cfg, transports); err != nil {
		r.logger.Error("config reload failed: transports did not restart", zap.Error(err))
		return
	}
	if err := r.plugins.Reload(ctx, cfg); err != nil {
		r.logger.Error("config reload failed: plugins did not restart", zap.Error(err))
		return
	}

	r.logger.Info("config reloaded", zap.String("path", r.cfgPath))
}

const defaultStopTimeout = 30 * time.Second
