package powerplan

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	validation "github.com/invopop/validation"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const (
	pluginID = "powerplan_windows"
	entityID = "powerplan.mode"
)

var modeKeyUnsafe = regexp.MustCompile(`[^a-z0-9]+`)
var modeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Plugin{
		cfg:    cfg,
		logger: ctx.Logger(),
	}, nil
}

type Config struct {
	Enabled bool            `mapstructure:"enabled"`
	Modes   map[string]Mode `mapstructure:"modes"`
	Timeout time.Duration   `mapstructure:"timeout"`
}

type Mode struct {
	Name string `mapstructure:"name"`
	GUID string `mapstructure:"guid"`
}

func (c Config) Validate() error {
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	for key, mode := range c.Modes {
		if !modeKeyPattern.MatchString(key) {
			return fmt.Errorf("modes.%s: mode key must match %s", key, modeKeyPattern.String())
		}
		if err := validation.ValidateStruct(&mode,
			validation.Field(&mode.Name, validation.Required),
			validation.Field(&mode.GUID, validation.Required),
		); err != nil {
			return fmt.Errorf("modes.%s: %w", key, err)
		}
	}
	return nil
}

type Plugin struct {
	cfg    Config
	modes  map[string]Mode
	host   plugin.PluginHost
	logger *zap.Logger
}

func (p *Plugin) ID() string {
	return pluginID
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	p.host = host
	modes, err := p.resolveModes(ctx)
	if err != nil {
		return err
	}
	if len(modes) == 0 {
		return fmt.Errorf("no power plans available")
	}
	p.modes = modes
	options := p.options()
	_, err = host.RegisterEntity(entity.Entity{
		ID:      entityID,
		Name:    "Power Plan",
		Kind:    entity.KindSelect,
		Options: options,
		Icon:    "mdi:power-plug",
	})
	if err != nil {
		return err
	}
	if err := host.SubscribeCommand(entityID, p.handleCommand); err != nil {
		return err
	}
	if err := host.SetAvailability(entityID, entity.AvailabilityOnline); err != nil {
		return err
	}
	if active, err := p.activeMode(ctx); err == nil && active != "" {
		_ = host.PublishState(entityID, entity.OptionName(options, active))
	} else if err != nil {
		p.logger.Debug("active power plan could not be detected", zap.Error(err))
	}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) handleCommand(ctx context.Context, command plugin.Command) error {
	value, ok := command.Payload.(string)
	if !ok || value == "" {
		return fmt.Errorf("powerplan command payload must be a string option")
	}

	modeID, ok := entity.OptionValue(p.options(), value)
	if !ok {
		return fmt.Errorf("unknown power plan option %q", value)
	}
	mode, ok := p.modes[modeID]
	if !ok {
		return fmt.Errorf("power plan %q is no longer available", modeID)
	}

	timeout := p.cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "powercfg.exe", "/S", mode.GUID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = p.host.SetAvailability(entityID, entity.AvailabilityUnavailable)
		return fmt.Errorf("set power plan %q: %w: %s", modeID, err, strings.TrimSpace(stderr.String()))
	}

	_ = p.host.SetAvailability(entityID, entity.AvailabilityOnline)
	return p.host.PublishState(entityID, entity.OptionName(p.options(), modeID))
}

func (p *Plugin) activeMode(ctx context.Context) (string, error) {
	timeout := p.cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := exec.CommandContext(cmdCtx, "powercfg.exe", "/GETACTIVESCHEME").CombinedOutput()
	if err != nil {
		return "", err
	}
	text := strings.ToLower(string(output))
	for key, mode := range p.modes {
		if strings.Contains(text, strings.ToLower(mode.GUID)) {
			return key, nil
		}
	}
	return "", nil
}

func (p *Plugin) resolveModes(ctx context.Context) (map[string]Mode, error) {
	modes := make(map[string]Mode, len(p.cfg.Modes))
	for key, mode := range p.cfg.Modes {
		modes[key] = mode
	}

	discovered, err := discoverPowerPlans(ctx)
	if err != nil {
		if len(modes) == 0 {
			return nil, fmt.Errorf("discover power plans: %w", err)
		}
		p.logger.Warn("power plan discovery failed; using configured modes", zap.Error(err))
		return modes, nil
	}

	mergeDiscoveredModes(modes, discovered)
	return modes, nil
}

func mergeDiscoveredModes(modes map[string]Mode, discovered []Mode) {
	for _, mode := range discovered {
		if hasModeGUID(modes, mode.GUID) {
			continue
		}
		key := uniqueModeKey(modes, mode.Name, mode.GUID)
		modes[key] = mode
	}
}

func hasModeGUID(modes map[string]Mode, guid string) bool {
	for _, mode := range modes {
		if strings.EqualFold(mode.GUID, guid) {
			return true
		}
	}
	return false
}

func uniqueModeKey(modes map[string]Mode, name, guid string) string {
	base := modeKeyUnsafe.ReplaceAllString(strings.ToLower(name), "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "plan_" + shortGUID(guid)
	}
	key := base
	for {
		if _, ok := modes[key]; !ok {
			return key
		}
		key = base + "_" + shortGUID(guid)
		if _, ok := modes[key]; !ok {
			return key
		}
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s_%s_%d", base, shortGUID(guid), i)
			if _, ok := modes[candidate]; !ok {
				return candidate
			}
		}
	}
}

func shortGUID(guid string) string {
	guid = strings.ReplaceAll(strings.ToLower(guid), "-", "")
	if len(guid) >= 8 {
		return guid[:8]
	}
	if guid == "" {
		return "unknown"
	}
	return guid
}

func (p *Plugin) timeout() time.Duration {
	if p.cfg.Timeout != 0 {
		return p.cfg.Timeout
	}
	return 10 * time.Second
}

func (p *Plugin) options() []entity.Option {
	keys := make([]string, 0, len(p.modes))
	for key := range p.modes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := strings.ToLower(p.modes[keys[i]].Name)
		right := strings.ToLower(p.modes[keys[j]].Name)
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})

	options := make([]entity.Option, 0, len(keys))
	for _, key := range keys {
		mode := p.modes[key]
		options = append(options, entity.Option{Value: key, Name: mode.Name})
	}
	return options
}
