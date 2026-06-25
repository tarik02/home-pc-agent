package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	validation "github.com/invopop/validation"
)

const (
	pluginID = "runner"

	kindButton = "button"
	kindSelect = "select"
	kindSwitch = "switch"

	inputNone       = "none"
	inputStructured = "structured"
	inputJSON       = "json"

	deliveryArgs      = "args"
	deliveryStdinJSON = "stdin_json"
	deliveryEnv       = "env"

	interpreterPwsh        = "pwsh"
	interpreterPowerShell  = "powershell"
	interpreterCmd         = "cmd"
	interpreterExe         = "exe"
	interpreterPowerShellE = "powershell.exe"

	stateNone        = "none"
	stateLastSuccess = "last_success"
	stateGetter      = "getter"
	afterSetRefresh  = "refresh"
	afterSetLast     = "last_success"

	getterFormatPlain = "plain"
	getterFormatJSON  = "json"

	stateUnknown = "unknown"
)

var (
	actionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	entityIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	paramNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Config struct {
	Enabled              bool                    `mapstructure:"enabled"`
	DefaultTimeout       time.Duration           `mapstructure:"default_timeout"`
	DefaultGetterRefresh time.Duration           `mapstructure:"getter_refresh"`
	DefaultCommand       string                  `mapstructure:"default_command"`
	DefaultArgs          []string                `mapstructure:"default_args"`
	Actions              map[string]ActionConfig `mapstructure:"actions"`
}

type ActionConfig struct {
	Name          string                  `mapstructure:"name"`
	Kind          string                  `mapstructure:"kind"`
	EntityID      string                  `mapstructure:"entity_id"`
	Icon          string                  `mapstructure:"icon"`
	Tags          []string                `mapstructure:"tags"`
	RefreshTags   []string                `mapstructure:"refresh_tags"`
	Optional      bool                    `mapstructure:"optional"`
	Timeout       time.Duration           `mapstructure:"timeout"`
	State         StateConfig             `mapstructure:"state"`
	Setter        SetterConfig            `mapstructure:"setter"`
	Getter        GetterConfig            `mapstructure:"getter"`
	OptionsGetter *GetterConfig           `mapstructure:"options_getter"`
	Output        OutputConfig            `mapstructure:"output"`
	Options       map[string]OptionConfig `mapstructure:"options"`
}

type StateConfig struct {
	Source   string `mapstructure:"source"`
	AfterSet string `mapstructure:"after_set"`
}

type SetterConfig struct {
	Path        string                     `mapstructure:"path"`
	Command     string                     `mapstructure:"command"`
	Args        []string                   `mapstructure:"args"`
	Interpreter string                     `mapstructure:"interpreter"`
	StaticArgs  []string                   `mapstructure:"static_args"`
	Input       string                     `mapstructure:"input"`
	Delivery    string                     `mapstructure:"delivery"`
	Timeout     time.Duration              `mapstructure:"timeout"`
	Parameters  map[string]ParameterSchema `mapstructure:"parameters"`
	JSON        JSONConfig                 `mapstructure:"json"`
	On          SideConfig                 `mapstructure:"on"`
	Off         SideConfig                 `mapstructure:"off"`
}

type SideConfig struct {
	Parameters map[string]any `mapstructure:"parameters"`
}

type GetterConfig struct {
	Path        string        `mapstructure:"path"`
	Command     string        `mapstructure:"command"`
	Args        []string      `mapstructure:"args"`
	Interpreter string        `mapstructure:"interpreter"`
	StaticArgs  []string      `mapstructure:"static_args"`
	Timeout     time.Duration `mapstructure:"timeout"`
	Format      string        `mapstructure:"format"`
	JSONPath    string        `mapstructure:"json_path"`
	Refresh     time.Duration `mapstructure:"refresh"`
}

type OutputConfig struct {
	Capture  bool `mapstructure:"capture"`
	MaxBytes int  `mapstructure:"max_bytes"`
}

type JSONConfig struct {
	MaxBytes int  `mapstructure:"max_bytes"`
	Strict   bool `mapstructure:"strict"`
}

type OptionConfig struct {
	Name       string         `mapstructure:"name"`
	Parameters map[string]any `mapstructure:"parameters"`
}

type ParameterSchema struct {
	Type     string   `mapstructure:"type"`
	Required bool     `mapstructure:"required"`
	Enum     []string `mapstructure:"enum"`
	Pattern  string   `mapstructure:"pattern"`
	Min      *int     `mapstructure:"min"`
	Max      *int     `mapstructure:"max"`
}

func (c Config) WithDefaults() Config {
	return c.withDefaults()
}

func (c Config) withDefaults() Config {
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 120 * time.Second
	}
	for key, action := range c.Actions {
		action = action.withDefaults(c)
		c.Actions[key] = action
	}
	return c
}

func (a ActionConfig) withDefaults(cfg Config) ActionConfig {
	if a.Timeout == 0 {
		a.Timeout = cfg.DefaultTimeout
	}
	if a.State.Source == "" {
		a.State.Source = stateLastSuccess
	}
	if a.State.AfterSet == "" {
		a.State.AfterSet = afterSetRefresh
	}
	if a.Setter.Input == "" {
		a.Setter.Input = inputNone
	}
	if a.Setter.Delivery == "" {
		a.Setter.Delivery = deliveryArgs
	}
	if a.Setter.Timeout == 0 {
		a.Setter.Timeout = a.Timeout
	}
	a.Setter = a.Setter.withPluginDefaults(cfg)
	a.Getter = a.Getter.withPluginDefaults(cfg)
	if a.OptionsGetter != nil {
		og := a.OptionsGetter.withPluginDefaults(cfg)
		if og.Format == "" {
			og.Format = getterFormatJSON
		}
		if og.Timeout == 0 {
			og.Timeout = 30 * time.Second
		}
		a.OptionsGetter = &og
	}
	if a.Getter.Format == "" {
		a.Getter.Format = getterFormatPlain
	}
	if a.Getter.Timeout == 0 {
		a.Getter.Timeout = 30 * time.Second
	}
	if a.State.Source == stateGetter && a.Getter.Refresh == 0 && cfg.DefaultGetterRefresh > 0 {
		a.Getter.Refresh = cfg.DefaultGetterRefresh
	}
	if a.Output.MaxBytes == 0 {
		a.Output.MaxBytes = 65536
	}
	if !a.Output.Capture {
		a.Output.Capture = true
	}
	if a.EntityID == "" {
		a.EntityID = ""
	}
	return a
}

func (s SetterConfig) withPluginDefaults(cfg Config) SetterConfig {
	if strings.TrimSpace(s.Command) == "" {
		s.Command = cfg.DefaultCommand
	}
	if len(s.Args) == 0 && len(cfg.DefaultArgs) > 0 {
		s.Args = append([]string{}, cfg.DefaultArgs...)
	}
	return s
}

func (g GetterConfig) withPluginDefaults(cfg Config) GetterConfig {
	if strings.TrimSpace(g.Command) == "" {
		g.Command = cfg.DefaultCommand
	}
	if len(g.Args) == 0 && len(cfg.DefaultArgs) > 0 {
		g.Args = append([]string{}, cfg.DefaultArgs...)
	}
	return g
}

func (a ActionConfig) resolvedEntityID(actionKey string) string {
	if a.EntityID != "" {
		return a.EntityID
	}
	return pluginID + "." + actionKey
}

func (c Config) Validate() error {
	if len(c.Actions) == 0 {
		return fmt.Errorf("at least one action is required when runner is enabled")
	}
	for key, action := range c.Actions {
		if err := action.validate(key, c); err != nil {
			return err
		}
	}
	return nil
}

func (a ActionConfig) validate(actionKey string, cfg Config) error {
	prefix := fmt.Sprintf("actions.%s", actionKey)
	if !actionKeyPattern.MatchString(actionKey) {
		return fmt.Errorf("%s: action key must match %s", prefix, actionKeyPattern.String())
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("%s.name: required", prefix)
	}
	entityID := a.resolvedEntityID(actionKey)
	if !entityIDPattern.MatchString(entityID) {
		return fmt.Errorf("%s.entity_id %q must match %s", prefix, entityID, entityIDPattern.String())
	}

	switch a.Kind {
	case kindButton, kindSelect, kindSwitch:
	default:
		return fmt.Errorf("%s.kind: must be button, select, or switch", prefix)
	}

	if err := a.State.validate(prefix); err != nil {
		return err
	}
	if err := a.Setter.validate(prefix+".setter", cfg); err != nil {
		return err
	}
	if a.State.Source == stateGetter {
		if err := a.Getter.validate(prefix+".getter", cfg); err != nil {
			return err
		}
	}
	if a.hasOptionsGetter() {
		if err := a.OptionsGetter.validateOptionsList(prefix + ".options_getter"); err != nil {
			return err
		}
	}
	if a.State.AfterSet != afterSetRefresh && a.State.AfterSet != afterSetLast {
		return fmt.Errorf("%s.state.after_set: must be refresh or last_success", prefix)
	}

	switch a.Kind {
	case kindSelect:
		if len(a.Options) == 0 && !a.hasOptionsGetter() {
			return fmt.Errorf("%s: select actions require options or options_getter", prefix)
		}
		if len(a.Options) > 0 {
			for optKey, opt := range a.Options {
				if !actionKeyPattern.MatchString(optKey) {
					return fmt.Errorf("%s.options.%s: option key must match %s", prefix, optKey, actionKeyPattern.String())
				}
				if strings.TrimSpace(opt.Name) == "" {
					return fmt.Errorf("%s.options.%s.name: required", prefix, optKey)
				}
			}
		}
	case kindSwitch:
		if len(a.Setter.On.Parameters) == 0 {
			return fmt.Errorf("%s.setter.on.parameters: required", prefix)
		}
		if len(a.Setter.Off.Parameters) == 0 {
			return fmt.Errorf("%s.setter.off.parameters: required", prefix)
		}
	case kindButton:
		if a.Setter.Input == inputJSON && a.Setter.JSON.MaxBytes == 0 {
			return fmt.Errorf("%s.setter.json.max_bytes: required when input is json", prefix)
		}
	}

	for paramKey, schema := range a.Setter.Parameters {
		if err := schema.validate(prefix + ".setter.parameters." + paramKey); err != nil {
			return err
		}
	}
	for i, tag := range a.Tags {
		if err := validateTag(prefix+".tags", tag, i); err != nil {
			return err
		}
	}
	for i, tag := range a.RefreshTags {
		if err := validateTag(prefix+".refresh_tags", tag, i); err != nil {
			return err
		}
	}
	return nil
}

func validateTag(prefix, tag string, index int) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return fmt.Errorf("%s[%d]: required", prefix, index)
	}
	if !actionKeyPattern.MatchString(tag) {
		return fmt.Errorf("%s[%d] %q: must match %s", prefix, index, tag, actionKeyPattern.String())
	}
	return nil
}

func (s StateConfig) validate(prefix string) error {
	switch s.Source {
	case stateNone, stateLastSuccess, stateGetter:
	default:
		return fmt.Errorf("%s.state.source: must be none, last_success, or getter", prefix)
	}
	return nil
}

func (s SetterConfig) validate(prefix string, cfg Config) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("%s.path: required", prefix)
	}
	if err := ensureFileExists(s.Path); err != nil {
		return fmt.Errorf("%s.path: %w", prefix, err)
	}
	if err := validateCommandOrInterpreter(prefix, resolveCommand(s.Command, cfg.DefaultCommand), s.Interpreter); err != nil {
		return err
	}
	switch s.Input {
	case inputNone, inputStructured, inputJSON:
	default:
		return fmt.Errorf("%s.input: must be none, structured, or json", prefix)
	}
	switch s.Delivery {
	case deliveryArgs, deliveryStdinJSON, deliveryEnv:
	default:
		return fmt.Errorf("%s.delivery: must be args, stdin_json, or env", prefix)
	}
	if s.Input == inputJSON && s.JSON.MaxBytes <= 0 {
		return fmt.Errorf("%s.json.max_bytes: required when input is json", prefix)
	}
	return nil
}

func (g GetterConfig) validate(prefix string, cfg Config) error {
	if strings.TrimSpace(g.Path) == "" {
		return fmt.Errorf("%s.path: required", prefix)
	}
	if err := ensureFileExists(g.Path); err != nil {
		return fmt.Errorf("%s.path: %w", prefix, err)
	}
	if err := validateCommandOrInterpreter(prefix, resolveCommand(g.Command, cfg.DefaultCommand), g.Interpreter); err != nil {
		return err
	}
	switch g.Format {
	case getterFormatPlain, getterFormatJSON:
	default:
		return fmt.Errorf("%s.format: must be plain or json", prefix)
	}
	if g.Format == getterFormatJSON && strings.TrimSpace(g.JSONPath) == "" {
		return fmt.Errorf("%s.json_path: required when format is json", prefix)
	}
	return nil
}

func (g GetterConfig) validateOptionsList(prefix string) error {
	if strings.TrimSpace(g.Path) == "" {
		return fmt.Errorf("%s.path: required", prefix)
	}
	if err := ensureFileExists(g.Path); err != nil {
		return fmt.Errorf("%s.path: %w", prefix, err)
	}
	if g.Format != "" && g.Format != getterFormatJSON {
		return fmt.Errorf("%s.format: must be json", prefix)
	}
	return nil
}

func (p ParameterSchema) validate(prefix string) error {
	switch p.Type {
	case "string", "integer", "number", "boolean":
	default:
		return fmt.Errorf("%s.type: must be string, integer, number, or boolean", prefix)
	}
	if p.Pattern != "" {
		if _, err := regexp.Compile(p.Pattern); err != nil {
			return fmt.Errorf("%s.pattern: %w", prefix, err)
		}
	}
	return nil
}

func resolveCommand(command, defaultCommand string) string {
	command = strings.TrimSpace(command)
	if command != "" {
		return command
	}
	return strings.TrimSpace(defaultCommand)
}

func validateCommandOrInterpreter(prefix, command, interpreter string) error {
	if command != "" {
		if err := ensureFileExists(command); err != nil {
			return fmt.Errorf("%s.command: %w", prefix, err)
		}
		return nil
	}
	if err := validation.Validate(interpreter, validation.Required, validation.In(
		interpreterPwsh,
		interpreterPowerShell,
		interpreterPowerShellE,
		interpreterCmd,
		interpreterExe,
	)); err != nil {
		return fmt.Errorf("%s.interpreter: %w", prefix, err)
	}
	return nil
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

func (a ActionConfig) entityCategory() string {
	if a.State.Source == stateNone && (a.Kind == kindSelect || a.Kind == kindButton) {
		return "config"
	}
	return ""
}

func (a ActionConfig) usesGetterRefresh() bool {
	return a.State.Source == stateGetter && a.Getter.Refresh > 0
}
