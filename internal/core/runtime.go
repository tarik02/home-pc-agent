package core

import (
	"context"
	"errors"
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
	logger          *zap.Logger
	factories       []plugin.Factory
	buildTransports TransportBuilder
	validateConfig  ConfigValidator
	configWatcher   *config.Watcher
	current         *runtimeGeneration
	failures        chan error

	mu sync.Mutex
}

type runtimeGeneration struct {
	cfg        *config.Config
	plugins    *PluginManager
	transports *TransportManager
}

func NewRuntime(cfgPath string, cfg *config.Config, logger *zap.Logger, factories []plugin.Factory, buildTransports TransportBuilder, transports []coretransport.Transport, validateConfig ConfigValidator) (*Runtime, error) {
	if validateConfig == nil {
		return nil, fmt.Errorf("validateConfig is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if buildTransports == nil {
		return nil, fmt.Errorf("buildTransports is required")
	}
	runtime := &Runtime{
		cfgPath:         cfgPath,
		logger:          logger,
		factories:       factories,
		buildTransports: buildTransports,
		validateConfig:  validateConfig,
		failures:        make(chan error, 1),
	}
	runtime.current = runtime.newGeneration(cfg, transports)
	return runtime, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	watcher, err := config.Watch(r.cfgPath, func() {
		r.reloadFromDisk(ctx)
	})
	if err != nil {
		return fmt.Errorf("watch config %q: %w", r.cfgPath, err)
	}
	r.configWatcher = watcher
	defer func() {
		_ = r.configWatcher.Close()
	}()

	r.mu.Lock()
	err = r.current.Start(ctx)
	r.mu.Unlock()
	if err != nil {
		return err
	}

	var runtimeErr error
	select {
	case <-ctx.Done():
	case runtimeErr = <-r.failures:
	}
	r.logger.Info("stopping runtime")

	stopCtx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
	defer cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(runtimeErr, r.current.Stop(stopCtx))
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

	transports := r.buildTransports(cfg, r.logger)
	candidate := r.newGeneration(cfg, transports)
	previous := r.current

	stopCtx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
	defer cancel()
	if err := previous.Stop(stopCtx); err != nil {
		restoreErr := r.restoreGeneration(ctx, previous)
		r.logger.Error("config reload aborted: failed to stop current runtime",
			zap.NamedError("stop_error", err),
			zap.NamedError("restore_error", restoreErr),
		)
		return
	}

	if err := candidate.Start(ctx); err != nil {
		candidateStopCtx, candidateStopCancel := context.WithTimeout(context.Background(), defaultStopTimeout)
		candidateStopErr := candidate.Stop(candidateStopCtx)
		candidateStopCancel()
		restoreErr := r.restoreGeneration(ctx, previous)
		r.logger.Error("config reload failed; restored previous runtime",
			zap.Error(err),
			zap.NamedError("candidate_cleanup_error", candidateStopErr),
			zap.NamedError("restore_error", restoreErr),
		)
		return
	}

	r.current = candidate
	r.logger.Info("config reloaded", zap.String("path", r.cfgPath))
}

// A failed stop may already have cancelled the transport's workers. Starting that
// object again can skip it as "started" while its MQTT connection stays online.
func (r *Runtime) restoreGeneration(ctx context.Context, previous *runtimeGeneration) error {
	cfg := previous.cfg
	restored := r.newGeneration(cfg, r.buildTransports(cfg, r.logger))
	r.current = restored
	if err := restored.Start(ctx); err != nil {
		failure := fmt.Errorf("restore previous runtime: %w", err)
		select {
		case r.failures <- failure:
		default:
		}
		return failure
	}
	return nil
}

func (r *Runtime) newGeneration(cfg *config.Config, transports []coretransport.Transport) *runtimeGeneration {
	bus := events.NewBus()
	registry := NewEntityRegistry(bus)
	router := NewCommandRouter()
	return &runtimeGeneration{
		cfg:        cfg,
		plugins:    NewPluginManager(cfg, registry, router, bus, r.logger, r.factories),
		transports: NewTransportManager(cfg, registry, router, bus, r.logger, transports),
	}
}

func (g *runtimeGeneration) Start(ctx context.Context) error {
	if err := g.transports.Start(ctx); err != nil {
		return err
	}
	if err := g.plugins.Start(ctx); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
		stopErr := g.transports.Stop(stopCtx)
		cancel()
		if stopErr != nil {
			return fmt.Errorf("%w; stop transports after plugin failure: %v", err, stopErr)
		}
		return err
	}
	return nil
}

func (g *runtimeGeneration) Stop(ctx context.Context) error {
	pluginErr := g.plugins.Stop(ctx)
	transportErr := g.transports.Stop(ctx)
	return errors.Join(pluginErr, transportErr)
}

const defaultStopTimeout = 30 * time.Second
