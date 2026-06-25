package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	validation "github.com/invopop/validation"
	"github.com/spf13/viper"

	"github.com/tarik02/home-pc-agent/internal/plugins/runner"
)

type Config struct {
	Agent      AgentConfig     `mapstructure:"agent"`
	Transports TransportConfig `mapstructure:"transports"`
	Plugins    map[string]any  `mapstructure:"plugins"`
}

type AgentConfig struct {
	ID      string `mapstructure:"id"`
	Name    string `mapstructure:"name"`
	DataDir string `mapstructure:"data_dir"`
}

type TransportConfig struct {
	MQTT MQTTConfig `mapstructure:"mqtt"`
}

type MQTTConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	Broker         string        `mapstructure:"broker"`
	ClientID       string        `mapstructure:"client_id"`
	Username       string        `mapstructure:"username"`
	Password       string        `mapstructure:"password"`
	TopicPrefix    string        `mapstructure:"topic_prefix"`
	DiscoveryBase  string        `mapstructure:"discovery_base"`
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"`
}

type PowerPlanConfig struct {
	Enabled bool                     `mapstructure:"enabled"`
	Modes   map[string]PowerPlanMode `mapstructure:"modes"`
	Timeout time.Duration            `mapstructure:"timeout"`
}

type PowerPlanMode struct {
	Name string `mapstructure:"name"`
	GUID string `mapstructure:"guid"`
}

type FanControlConfig struct {
	Enabled    bool                         `mapstructure:"enabled"`
	ExePath    string                       `mapstructure:"exe_path"`
	ConfigPath string                       `mapstructure:"config_path"`
	Profiles   map[string]FanControlProfile `mapstructure:"profiles"`
	Timeout    time.Duration                `mapstructure:"timeout"`
}

type FanControlProfile struct {
	Name       string `mapstructure:"name"`
	ConfigName string `mapstructure:"config_name"`
	SourcePath string `mapstructure:"source_path"`
}

type SessionConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

var (
	agentIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	topicPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/_-]*$`)
	optionKeyPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

const (
	defaultFanControlExePath    = `C:\Program Files (x86)\FanControl\FanControl.exe`
	defaultFanControlConfigPath = `C:\Program Files (x86)\FanControl\Configurations\userConfig.json`
)

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is required")
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	v.SetDefault("transports.mqtt.discovery_base", "homeassistant")
	v.SetDefault("transports.mqtt.connect_timeout", "5s")
	v.SetDefault("plugins.fancontrol.exe_path", defaultFanControlExePath)
	v.SetDefault("plugins.fancontrol.config_path", defaultFanControlConfigPath)
	v.SetDefault("plugins.fancontrol.timeout", "20s")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := decodeMap(v.AllSettings(), &cfg); err != nil {
		return nil, fmt.Errorf("decode config %q: %w", path, err)
	}
	if cfg.Plugins == nil {
		cfg.Plugins = map[string]any{}
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}

	var errs []error
	if err := validation.ValidateStruct(&c.Agent,
		validation.Field(&c.Agent.ID, validation.Required, validation.Match(agentIDPattern)),
		validation.Field(&c.Agent.Name, validation.Required),
		validation.Field(&c.Agent.DataDir, validation.Required),
	); err != nil {
		errs = append(errs, fmt.Errorf("agent: %w", err))
	}

	if c.Transports.MQTT.Enabled {
		if err := validation.ValidateStruct(&c.Transports.MQTT,
			validation.Field(&c.Transports.MQTT.Broker, validation.Required),
			validation.Field(&c.Transports.MQTT.ClientID, validation.Required),
			validation.Field(&c.Transports.MQTT.TopicPrefix, validation.Required, validation.Match(topicPrefixPattern)),
		); err != nil {
			errs = append(errs, fmt.Errorf("transports.mqtt: %w", err))
		}
	}

	errs = append(errs, c.validatePlugins()...)
	return errors.Join(errs...)
}

func (c *Config) PluginRaw(id string) map[string]any {
	raw, ok := c.Plugins[id]
	if !ok {
		return nil
	}
	out, ok := normalizeMap(raw).(map[string]any)
	if !ok {
		return nil
	}
	return out
}

func (c *Config) PluginEnabled(id string) bool {
	raw := c.PluginRaw(id)
	if raw == nil {
		return false
	}
	value, ok := raw["enabled"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func (c *Config) EnabledPluginIDs() []string {
	ids := make([]string, 0, len(c.Plugins))
	for id := range c.Plugins {
		if c.PluginEnabled(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func DecodePlugin(raw map[string]any, target any) error {
	return decodeMap(raw, target)
}

func (c *Config) validatePlugins() []error {
	var errs []error
	known := map[string]struct{}{
		"powerplan":  {},
		"fancontrol": {},
		"session":    {},
		"runner":     {},
	}
	for id := range c.Plugins {
		if _, ok := known[id]; !ok {
			errs = append(errs, fmt.Errorf("plugins.%s: unknown builtin plugin", id))
		}
	}

	var powerPlan PowerPlanConfig
	if err := decodeOptionalPlugin(c.PluginRaw("powerplan"), &powerPlan); err != nil {
		errs = append(errs, fmt.Errorf("plugins.powerplan: %w", err))
	} else if powerPlan.Enabled {
		errs = append(errs, validatePowerPlan(powerPlan)...)
	}

	var fanControl FanControlConfig
	if err := decodeOptionalPlugin(c.PluginRaw("fancontrol"), &fanControl); err != nil {
		errs = append(errs, fmt.Errorf("plugins.fancontrol: %w", err))
	} else if fanControl.Enabled {
		errs = append(errs, validateFanControl(fanControl)...)
	}

	var session SessionConfig
	if err := decodeOptionalPlugin(c.PluginRaw("session"), &session); err != nil {
		errs = append(errs, fmt.Errorf("plugins.session: %w", err))
	}

	var runnerCfg runner.Config
	if err := decodeOptionalPlugin(c.PluginRaw("runner"), &runnerCfg); err != nil {
		errs = append(errs, fmt.Errorf("plugins.runner: %w", err))
	} else if c.PluginEnabled("runner") {
		runnerCfg = runnerCfg.WithDefaults()
		if err := runnerCfg.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("plugins.runner: %w", err))
		}
	}

	return errs
}

func validatePowerPlan(cfg PowerPlanConfig) []error {
	var errs []error
	for key, mode := range cfg.Modes {
		if !optionKeyPattern.MatchString(key) {
			errs = append(errs, fmt.Errorf("plugins.powerplan.modes.%s: mode key must match %s", key, optionKeyPattern.String()))
		}
		if strings.TrimSpace(mode.Name) == "" {
			errs = append(errs, fmt.Errorf("plugins.powerplan.modes.%s.name: required", key))
		}
		if strings.TrimSpace(mode.GUID) == "" {
			errs = append(errs, fmt.Errorf("plugins.powerplan.modes.%s.guid: required", key))
		}
	}
	return errs
}

func validateFanControl(cfg FanControlConfig) []error {
	var errs []error
	if cfg.ExePath == "" {
		cfg.ExePath = defaultFanControlExePath
	}
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = defaultFanControlConfigPath
	}
	for field, path := range map[string]string{
		"exe_path":    cfg.ExePath,
		"config_path": cfg.ConfigPath,
	} {
		if strings.TrimSpace(path) == "" {
			errs = append(errs, fmt.Errorf("plugins.fancontrol.%s: required", field))
			continue
		}
		if err := ensureFileExists(path); err != nil {
			errs = append(errs, fmt.Errorf("plugins.fancontrol.%s: %w", field, err))
		}
	}

	for key, profile := range cfg.Profiles {
		if !optionKeyPattern.MatchString(key) {
			errs = append(errs, fmt.Errorf("plugins.fancontrol.profiles.%s: profile key must match %s", key, optionKeyPattern.String()))
		}
		if strings.TrimSpace(profile.SourcePath) != "" {
			if err := ensureFileExists(profile.SourcePath); err != nil {
				errs = append(errs, fmt.Errorf("plugins.fancontrol.profiles.%s.source_path: %w", key, err))
			}
		}
		if strings.TrimSpace(profile.Name) == "" && strings.TrimSpace(profile.ConfigName) == "" && strings.TrimSpace(profile.SourcePath) == "" {
			errs = append(errs, fmt.Errorf("plugins.fancontrol.profiles.%s: name, config_name, or source_path is required", key))
		}
		if strings.TrimSpace(profile.ConfigName) != "" && strings.ContainsAny(profile.ConfigName, `/\`) {
			errs = append(errs, fmt.Errorf("plugins.fancontrol.profiles.%s.config_name: must be a filename, not a path", key))
		}
	}
	return errs
}

func decodeOptionalPlugin(raw map[string]any, target any) error {
	if raw == nil {
		raw = map[string]any{}
	}
	return decodeMap(raw, target)
}

func decodeMap(raw any, target any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
		),
		ErrorUnused:      true,
		Result:           target,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(normalizeMap(raw))
}

func normalizeMap(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = normalizeMap(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[fmt.Sprint(k)] = normalizeMap(v)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, normalizeMap(v))
		}
		return out
	default:
		return value
	}
}

func ensureFileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, expected a file", path)
	}
	return nil
}
