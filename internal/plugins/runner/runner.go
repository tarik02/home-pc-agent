package runner

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

func NewFactory() plugin.Factory {
	factory := plugin.ConfigFactory[Config](
		plugin.Descriptor{ID: pluginID},
		func(cfg Config) (Config, error) {
			cfg = cfg.WithDefaults()
			if cfg.Enabled {
				if err := cfg.Validate(); err != nil {
					return Config{}, err
				}
			}
			return cfg, nil
		},
		func(cfg Config, logger *zap.Logger) plugin.Plugin {
			return &Plugin{
				cfg:      cfg,
				logger:   logger,
				runtimes: make(map[string]*actionRuntime, len(cfg.Actions)),
			}
		},
	)
	factory.ConfiguredEntityIDs = func(raw map[string]any) ([]string, error) {
		var cfg Config
		if err := config.DecodePlugin(raw, &cfg); err != nil {
			return nil, err
		}
		entityIDs := make([]string, 0, len(cfg.Actions))
		for key, action := range cfg.Actions {
			entityIDs = append(entityIDs, action.resolvedEntityID(key))
		}
		sort.Strings(entityIDs)
		return entityIDs, nil
	}
	return factory
}

type actionRuntime struct {
	action            ActionConfig
	actionKey         string
	entityID          string
	mu                sync.RWMutex
	discoveredOptions map[string]OptionConfig
}

type Plugin struct {
	cfg    Config
	host   plugin.PluginHost
	logger *zap.Logger

	mu       sync.RWMutex
	runtimes map[string]*actionRuntime
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	p.host = host

	keys := make([]string, 0, len(p.cfg.Actions))
	for key := range p.cfg.Actions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		action := p.cfg.Actions[key]
		runtime := &actionRuntime{
			action:    action,
			actionKey: key,
			entityID:  action.resolvedEntityID(key),
		}
		p.mu.Lock()
		p.runtimes[runtime.entityID] = runtime
		p.mu.Unlock()

		if err := p.discoverOptions(ctx, runtime); err != nil {
			if action.Optional {
				p.logger.Warn("runner optional action unavailable; skipping entity", zap.String("action", key), zap.Error(err))
				p.mu.Lock()
				delete(p.runtimes, runtime.entityID)
				p.mu.Unlock()
				continue
			}
			return fmt.Errorf("actions.%s options_getter: %w", key, err)
		}
		if action.Kind == kindSelect && len(runtime.optionMap()) == 0 {
			if action.Optional {
				p.logger.Warn("runner optional action has no options; skipping entity", zap.String("action", key))
				p.mu.Lock()
				delete(p.runtimes, runtime.entityID)
				p.mu.Unlock()
				continue
			}
			return fmt.Errorf("actions.%s: select action has no options", key)
		}

		ent, err := p.buildEntity(runtime)
		if err != nil {
			return err
		}
		if err := host.RegisterEntity(ent); err != nil {
			return err
		}
		entityID := runtime.entityID
		if err := host.SubscribeCommand(entityID, func(cmdCtx context.Context, command plugin.Command) error {
			return p.handleCommand(cmdCtx, entityID, command)
		}); err != nil {
			return err
		}
		if err := host.SetAvailability(entityID, entity.AvailabilityOnline); err != nil {
			return err
		}
		if err := p.publishInitialState(ctx, runtime); err != nil {
			p.logger.Warn("runner initial state publish failed", zap.String("entity_id", entityID), zap.Error(err))
		}
		if action.usesGetterRefresh() {
			p.startGetterRefresh(runtime)
		}
		if action.hasOptionsGetter() && action.OptionsGetter.Refresh > 0 {
			p.startOptionsRefresh(runtime)
		}
	}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) buildEntity(runtime *actionRuntime) (entity.Entity, error) {
	action := runtime.action
	ent := entity.Entity{
		ID:       runtime.entityID,
		Name:     action.Name,
		Kind:     entity.Kind(action.Kind),
		Icon:     action.Icon,
		Category: action.entityCategory(),
	}
	switch action.Kind {
	case kindSelect:
		ent.Options = optionNamesFromMap(runtime.optionMap())
	case kindSwitch:
		// no options
	case kindButton:
		// no options
	default:
		return entity.Entity{}, fmt.Errorf("unsupported kind %q", action.Kind)
	}
	return ent, nil
}

func (p *Plugin) publishInitialState(ctx context.Context, runtime *actionRuntime) error {
	if runtime.action.State.Source == stateGetter {
		value, err := p.runGetter(ctx, runtime)
		if err != nil {
			p.logger.Warn("runner getter failed during startup", zap.String("entity_id", runtime.entityID), zap.Error(err))
			return p.host.PublishState(runtime.entityID, initialEnvelope(runtime.action))
		}
		return p.host.PublishState(runtime.entityID, StateEnvelope{"state": formatHAState(runtime.action.Kind, value)})
	}
	return p.host.PublishState(runtime.entityID, initialEnvelope(runtime.action))
}

func (p *Plugin) startGetterRefresh(runtime *actionRuntime) {
	refresh := runtime.action.Getter.Refresh
	entityID := runtime.entityID
	p.host.Go(fmt.Sprintf("getter-%s", entityID), func(loopCtx context.Context) error {
		ticker := time.NewTicker(refresh)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return nil
			case <-ticker.C:
				if err := p.refreshGetterState(loopCtx, runtime); err != nil {
					p.logger.Debug("runner getter refresh failed", zap.String("entity_id", entityID), zap.Error(err))
				}
			}
		}
	})
}

func (p *Plugin) startOptionsRefresh(runtime *actionRuntime) {
	refresh := runtime.action.OptionsGetter.Refresh
	entityID := runtime.entityID
	p.host.Go(fmt.Sprintf("options-%s", entityID), func(loopCtx context.Context) error {
		ticker := time.NewTicker(refresh)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return nil
			case <-ticker.C:
				if err := p.refreshDiscoveredOptions(loopCtx, runtime); err != nil {
					p.logger.Debug("runner options refresh failed", zap.String("entity_id", entityID), zap.Error(err))
				}
			}
		}
	})
}

func (p *Plugin) handleCommand(ctx context.Context, entityID string, command plugin.Command) error {
	runtime := p.runtimeFor(entityID)
	if runtime == nil {
		return fmt.Errorf("unknown runner entity %q", entityID)
	}

	params, successState, err := p.resolveCommandParams(runtime, command)
	if err != nil {
		return err
	}

	timeout := runtime.action.Setter.Timeout
	if timeout == 0 {
		timeout = p.cfg.DefaultTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runCommand(cmdCtx, CommandSpec{
		Command:     runtime.action.Setter.Command,
		Args:        runtime.action.Setter.Args,
		Interpreter: runtime.action.Setter.Interpreter,
		Path:        runtime.action.Setter.Path,
		StaticArgs:  runtime.action.Setter.StaticArgs,
		Delivery:    runtime.action.Setter.Delivery,
		Params:      params,
		Output:      runtime.action.Output,
	})
	if err != nil {
		return fmt.Errorf("run %q: %w", runtime.actionKey, err)
	}
	if result.ExitCode != 0 {
		p.logger.Warn("runner setter failed",
			zap.String("entity_id", entityID),
			zap.String("action", runtime.actionKey),
			zap.Int("exit_code", result.ExitCode),
			zap.String("stderr", result.Stderr),
			zap.String("stdout", result.Stdout),
		)
		return fmt.Errorf("run %q: exit code %d: %s", runtime.actionKey, result.ExitCode, result.Stderr)
	}

	stateValue, err := p.resolveStateAfterRun(ctx, runtime, successState, result)
	if err != nil {
		return err
	}
	if err := p.host.PublishState(entityID, newStateEnvelope(runtime.action.Kind, stateValue, result)); err != nil {
		return err
	}
	if runtime.action.State.AfterSet == afterSetRefresh {
		tags := append([]string{}, runtime.action.effectiveRefreshTags()...)
		p.host.Go(fmt.Sprintf("runner-post-set-%s", entityID), func(loopCtx context.Context) error {
			select {
			case <-loopCtx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			p.refreshByTags(loopCtx, tags)
			return nil
		})
	}
	return nil
}

func (p *Plugin) resolveCommandParams(runtime *actionRuntime, command plugin.Command) (map[string]any, any, error) {
	action := runtime.action
	switch action.Kind {
	case kindButton:
		return p.resolveButtonParams(action, command)
	case kindSelect:
		return p.resolveSelectParams(runtime, command)
	case kindSwitch:
		return p.resolveSwitchParams(action, command)
	default:
		return nil, nil, fmt.Errorf("unsupported kind %q", action.Kind)
	}
}

func (p *Plugin) resolveButtonParams(action ActionConfig, command plugin.Command) (map[string]any, any, error) {
	switch action.Setter.Input {
	case inputNone:
		return map[string]any{}, nil, nil
	case inputStructured:
		params, err := payloadAsMap(command.Payload)
		if err != nil {
			return nil, nil, err
		}
		if err := validateStructuredParameters(action.Setter.Parameters, params, "command"); err != nil {
			return nil, nil, err
		}
		return params, nil, nil
	case inputJSON:
		params, err := decodePayloadMap(command.Raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decode json command: %w", err)
		}
		if err := validateJSONParameters(action.Setter.Parameters, params, action.Setter.JSON, command.Raw, "command"); err != nil {
			return nil, nil, err
		}
		return params, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported setter input %q", action.Setter.Input)
	}
}

func (p *Plugin) resolveSelectParams(runtime *actionRuntime, command plugin.Command) (map[string]any, any, error) {
	action := runtime.action
	options := runtime.optionMap()
	optionList := optionNamesFromMap(options)
	value, ok := command.Payload.(string)
	if !ok || value == "" {
		return nil, nil, fmt.Errorf("select command payload must be a string option")
	}
	optionKey, ok := entity.OptionValue(optionList, value)
	if !ok {
		return nil, nil, fmt.Errorf("unknown select option %q", value)
	}
	option, ok := options[optionKey]
	if !ok {
		return nil, nil, fmt.Errorf("select option %q is no longer configured", optionKey)
	}
	params := cloneParameters(option.Parameters)
	if action.Setter.Input == inputStructured && len(action.Setter.Parameters) > 0 {
		if err := validateStructuredParameters(action.Setter.Parameters, params, "option"); err != nil {
			return nil, nil, err
		}
	}
	display := entity.OptionName(optionList, optionKey)
	return params, display, nil
}

func (p *Plugin) resolveSwitchParams(action ActionConfig, command plugin.Command) (map[string]any, any, error) {
	enabled, err := normalizeSwitchState(command.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("switch command payload: %w", err)
	}
	var side SideConfig
	if enabled {
		side = action.Setter.On
	} else {
		side = action.Setter.Off
	}
	params := cloneParameters(side.Parameters)
	if action.Setter.Input == inputStructured && len(action.Setter.Parameters) > 0 {
		if err := validateStructuredParameters(action.Setter.Parameters, params, "switch"); err != nil {
			return nil, nil, err
		}
	}
	return params, enabled, nil
}

func (p *Plugin) resolveStateAfterRun(ctx context.Context, runtime *actionRuntime, successState any, result RunResult) (any, error) {
	action := runtime.action
	switch action.State.Source {
	case stateNone:
		if action.Kind == kindSelect && successState != nil {
			// Startup stays "unknown"; after a command publish what was invoked, not claimed reality.
			return successState, nil
		}
		if action.Kind == kindSelect {
			return stateUnknown, nil
		}
		return nil, nil
	case stateLastSuccess:
		return successState, nil
	case stateGetter:
		if action.State.AfterSet == afterSetLast {
			return successState, nil
		}
		if action.Kind == kindSelect && successState != nil {
			return successState, nil
		}
		value, err := p.runGetter(ctx, runtime)
		if err != nil {
			p.logger.Debug("runner getter after set failed; using last_success", zap.String("entity_id", runtime.entityID), zap.Error(err))
			return successState, nil
		}
		if action.Kind == kindSwitch {
			normalized, err := normalizeSwitchState(value)
			if err != nil {
				return successState, nil
			}
			return normalized, nil
		}
		return value, nil
	default:
		return successState, nil
	}
}

func (p *Plugin) runGetter(ctx context.Context, runtime *actionRuntime) (any, error) {
	action := runtime.action
	timeout := action.Getter.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runCommand(cmdCtx, CommandSpec{
		Command:     action.Getter.Command,
		Args:        action.Getter.Args,
		Interpreter: action.Getter.Interpreter,
		Path:        action.Getter.Path,
		StaticArgs:  action.Getter.StaticArgs,
		Delivery:    deliveryArgs,
		Params:      map[string]any{},
		Output:      OutputConfig{MaxBytes: action.Output.MaxBytes},
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("getter exit code %d: stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
	}
	value, err := parseGetterValue(result.Stdout, action.Getter)
	if err != nil {
		return nil, err
	}
	if action.Kind == kindSwitch {
		return normalizeSwitchState(value)
	}
	if action.Kind == kindSelect {
		return mapGetterToOptionName(action, runtime.optionMap(), fmt.Sprint(value))
	}
	return value, nil
}

func (p *Plugin) runtimeFor(entityID string) *actionRuntime {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.runtimes[entityID]
}
