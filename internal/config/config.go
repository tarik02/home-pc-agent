package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	validation "github.com/invopop/validation"
	"github.com/spf13/viper"
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

var (
	agentIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	topicPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/_-]*$`)
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

func (c *Config) DecodePluginSection(id string, target any) error {
	raw := c.PluginRaw(id)
	if raw == nil {
		raw = map[string]any{}
	}
	return DecodePlugin(raw, target)
}

func DecodePlugin(raw map[string]any, target any) error {
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
