package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
)

var optionKeySanitizer = regexp.MustCompile(`[^a-z0-9]+`)

func (a ActionConfig) hasOptionsGetter() bool {
	return a.OptionsGetter != nil && strings.TrimSpace(a.OptionsGetter.Path) != ""
}

func (r *actionRuntime) optionMap() map[string]OptionConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.discoveredOptions) > 0 {
		return cloneOptions(r.discoveredOptions)
	}
	return cloneOptions(r.action.Options)
}

func (p *Plugin) discoverOptions(ctx context.Context, runtime *actionRuntime) error {
	if !runtime.action.hasOptionsGetter() {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		opts, err := p.runOptionsGetter(ctx, runtime.action)
		if err != nil {
			lastErr = err
			continue
		}
		runtime.mu.Lock()
		runtime.discoveredOptions = cloneOptions(opts)
		runtime.mu.Unlock()
		return nil
	}
	if len(runtime.action.Options) > 0 {
		p.logger.Warn("runner options getter failed; using static options", zap.String("entity_id", runtime.entityID), zap.Error(lastErr))
		return nil
	}
	return lastErr
}

func (p *Plugin) refreshDiscoveredOptions(ctx context.Context, runtime *actionRuntime) error {
	if !runtime.action.hasOptionsGetter() {
		return nil
	}
	opts, err := p.runOptionsGetter(ctx, runtime.action)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	if optionsEqual(runtime.discoveredOptions, opts) {
		runtime.mu.Unlock()
		return nil
	}
	runtime.discoveredOptions = cloneOptions(opts)
	runtime.mu.Unlock()
	ent, err := p.buildEntity(runtime)
	if err != nil {
		return err
	}
	return p.host.UpdateEntity(ent)
}

func (p *Plugin) runOptionsGetter(ctx context.Context, action ActionConfig) (map[string]OptionConfig, error) {
	spec := action.optionsGetterSpec()
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runCommand(cmdCtx, CommandSpec{
		Command:     spec.Command,
		Args:        spec.Args,
		Interpreter: spec.Interpreter,
		Path:        spec.Path,
		StaticArgs:  spec.StaticArgs,
		Delivery:    deliveryArgs,
		Params:      map[string]any{},
		Output:      OutputConfig{MaxBytes: action.Output.MaxBytes},
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("options getter exit code %d: stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
	}
	return parseOptionsGetterJSON(result.Stdout)
}

func (a ActionConfig) optionsGetterSpec() GetterConfig {
	if a.OptionsGetter == nil {
		return GetterConfig{}
	}
	return *a.OptionsGetter
}

type discoveredOption struct {
	Value      string         `json:"value"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

func parseOptionsGetterJSON(stdout string) (map[string]OptionConfig, error) {
	text := strings.TrimSpace(stdout)
	if text == "" {
		return nil, fmt.Errorf("options getter stdout is empty")
	}

	var decoded []discoveredOption
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		var single discoveredOption
		if err := json.Unmarshal([]byte(text), &single); err != nil {
			return nil, fmt.Errorf("parse options getter json: %w", err)
		}
		decoded = []discoveredOption{single}
	}
	if len(decoded) == 0 {
		return map[string]OptionConfig{}, nil
	}

	options := make(map[string]OptionConfig, len(decoded))
	seenNames := make(map[string]struct{}, len(decoded))
	for i, item := range decoded {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("options getter option[%d]: name is required", i)
		}
		if _, ok := seenNames[name]; ok {
			continue
		}
		seenNames[name] = struct{}{}

		key := strings.TrimSpace(item.Value)
		if key == "" {
			key = slugOptionKey(name, i)
		}
		if _, exists := options[key]; exists {
			key = fmt.Sprintf("%s_%d", key, i)
		}
		params := item.Parameters
		if params == nil {
			params = map[string]any{}
		}
		options[key] = OptionConfig{
			Name:       name,
			Parameters: params,
		}
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("options getter returned no unique options")
	}
	return options, nil
}

func slugOptionKey(name string, index int) string {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.ReplaceAll(key, "×", "x")
	key = optionKeySanitizer.ReplaceAllString(key, "_")
	key = strings.Trim(key, "_")
	if key == "" {
		return fmt.Sprintf("opt_%d", index)
	}
	return key
}

func optionsEqual(a, b map[string]OptionConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for key, left := range a {
		right, ok := b[key]
		if !ok || left.Name != right.Name || !parametersEqual(left.Parameters, right.Parameters) {
			return false
		}
	}
	return true
}

func parametersEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, left := range a {
		right, ok := b[key]
		if !ok || fmt.Sprint(left) != fmt.Sprint(right) {
			return false
		}
	}
	return true
}

func cloneOptions(options map[string]OptionConfig) map[string]OptionConfig {
	if len(options) == 0 {
		return nil
	}
	cloned := make(map[string]OptionConfig, len(options))
	for key, option := range options {
		option.Parameters = cloneParameters(option.Parameters)
		cloned[key] = option
	}
	return cloned
}

func optionNamesFromMap(options map[string]OptionConfig) []entity.Option {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]entity.Option, 0, len(keys))
	for _, key := range keys {
		opt := options[key]
		name := opt.Name
		if name == "" {
			name = key
		}
		out = append(out, entity.Option{Value: key, Name: name})
	}
	return out
}
