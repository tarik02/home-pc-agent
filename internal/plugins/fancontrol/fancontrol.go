package fancontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const (
	pluginID                    = "fancontrol_windows"
	entityID                    = "fancontrol.profile"
	defaultFanControlExePath    = `C:\Program Files (x86)\FanControl\FanControl.exe`
	defaultFanControlConfigPath = `C:\Program Files (x86)\FanControl\Configurations\userConfig.json`
)

var (
	errFanControlIPCUnavailable = errors.New("FanControl IPC unavailable")
	profileKeyUnsafe            = regexp.MustCompile(`[^a-z0-9]+`)
	profileNameSeparator        = regexp.MustCompile(`[-_]+`)
)

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
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg, logger: ctx.Logger()}, nil
}

type Config struct {
	Enabled    bool               `mapstructure:"enabled"`
	ExePath    string             `mapstructure:"exe_path"`
	ConfigPath string             `mapstructure:"config_path"`
	Profiles   map[string]Profile `mapstructure:"profiles"`
	Timeout    time.Duration      `mapstructure:"timeout"`
}

type Profile struct {
	Name       string `mapstructure:"name"`
	ConfigName string `mapstructure:"config_name"`
	SourcePath string `mapstructure:"source_path"`
}

func (c Config) withDefaults() Config {
	if c.ExePath == "" {
		c.ExePath = defaultFanControlExePath
	}
	if c.ConfigPath == "" {
		c.ConfigPath = defaultFanControlConfigPath
	}
	if c.Timeout == 0 {
		c.Timeout = 20 * time.Second
	}
	return c
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ExePath) == "" {
		return fmt.Errorf("exe_path is required")
	}
	if strings.TrimSpace(c.ConfigPath) == "" {
		return fmt.Errorf("config_path is required")
	}
	for key, profile := range c.Profiles {
		if profile.ConfigName != "" && strings.ContainsAny(profile.ConfigName, `/\`) {
			return fmt.Errorf("profiles.%s.config_name must be a filename, not a path", key)
		}
		if profile.ConfigName == "" && profile.SourcePath == "" && profile.Name == "" {
			return fmt.Errorf("profiles.%s: name, config_name, or source_path is required", key)
		}
	}
	return nil
}

type Plugin struct {
	cfg      Config
	profiles map[string]Profile
	host     plugin.PluginHost
	logger   *zap.Logger
}

func (p *Plugin) ID() string {
	return pluginID
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	p.host = host
	profiles, err := p.resolveProfiles(ctx)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no FanControl profiles available")
	}
	p.profiles = profiles
	options := p.options()
	_, err = host.RegisterEntity(entity.Entity{
		ID:      entityID,
		Name:    "FanControl Profile",
		Kind:    entity.KindSelect,
		Options: options,
		Icon:    "mdi:fan",
	})
	if err != nil {
		return err
	}
	if err := host.SubscribeCommand(entityID, p.handleCommand); err != nil {
		return err
	}
	_ = host.SetAvailability(entityID, entity.AvailabilityOnline)
	p.publishActiveState(ctx, options)
	p.startStateRefresh(ctx, options)
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) publishActiveState(ctx context.Context, options []entity.Option) {
	activeCtx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	active, err := p.activeProfile(activeCtx)
	if err != nil {
		p.logger.Debug("active FanControl profile could not be detected", zap.Error(err))
		return
	}
	if active == "" {
		p.logger.Debug("active FanControl profile did not match a known profile")
		return
	}
	_ = p.host.PublishState(entityID, entity.OptionName(options, active))
}

func (p *Plugin) startStateRefresh(ctx context.Context, options []entity.Option) {
	p.host.Go("state-refresh", func(loopCtx context.Context) error {
		for _, delay := range []time.Duration{3 * time.Second, 15 * time.Second} {
			select {
			case <-loopCtx.Done():
				return nil
			case <-time.After(delay):
			}
			p.publishActiveState(loopCtx, options)
		}

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return nil
			case <-ticker.C:
				p.publishActiveState(loopCtx, options)
			}
		}
	})
}

func (p *Plugin) handleCommand(ctx context.Context, command plugin.Command) error {
	value, ok := command.Payload.(string)
	if !ok || value == "" {
		return fmt.Errorf("fancontrol command payload must be a string option")
	}
	profileID, ok := entity.OptionValue(p.options(), value)
	if !ok {
		return fmt.Errorf("unknown FanControl profile %q", value)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	if err := p.switchProfile(cmdCtx, profileID); err != nil {
		_ = p.host.SetAvailability(entityID, entity.AvailabilityUnavailable)
		return err
	}

	_ = p.host.SetAvailability(entityID, entity.AvailabilityOnline)
	active, err := p.activeProfile(cmdCtx)
	if err != nil || active == "" {
		if err != nil {
			p.logger.Debug("active FanControl profile could not be detected after switch", zap.Error(err))
		}
		active = profileID
	}
	return p.host.PublishState(entityID, entity.OptionName(p.options(), active))
}

func (p *Plugin) switchProfile(ctx context.Context, profileID string) error {
	profile, ok := p.profiles[profileID]
	if !ok {
		return fmt.Errorf("FanControl profile %q is no longer available", profileID)
	}

	configName := profile.configName(profileID)
	if configName != "" {
		conn, err := openFanControlIPC(ctx)
		if err == nil {
			defer conn.Close()
			rpc := newFanControlRPC(conn)
			if err := rpc.LoadConfig(ctx, configName); err != nil {
				return fmt.Errorf("switch FanControl profile through IPC: %w", err)
			}
			p.logger.Info("switched FanControl profile through IPC",
				zap.String("profile", profileID),
				zap.String("config", configName),
			)
			return nil
		} else if !errors.Is(err, errFanControlIPCUnavailable) {
			return fmt.Errorf("connect to FanControl IPC: %w", err)
		} else {
			p.logger.Debug("FanControl IPC unavailable; trying to start FanControl", zap.Error(err))
			if err := p.startFanControlAndLoad(ctx, configName); err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("FanControl profile %q has no config_name", profileID)
}

func (p *Plugin) activeProfile(ctx context.Context) (string, error) {
	conn, err := openFanControlIPC(ctx)
	if err == nil {
		defer conn.Close()
		rpc := newFanControlRPC(conn)
		configs, err := rpc.ListConfigs(ctx)
		if err != nil {
			p.logger.Debug("FanControl IPC current config could not be detected", zap.Error(err))
		} else if configs.CurrentConfig != "" {
			if profileID := p.profileByConfigName(configs.CurrentConfig); profileID != "" {
				return profileID, nil
			}
			p.logger.Debug("FanControl current config is not configured as a home-pc-agent profile",
				zap.String("current_config", configs.CurrentConfig),
			)
		}
	} else if !errors.Is(err, errFanControlIPCUnavailable) {
		p.logger.Debug("FanControl IPC current config could not be detected", zap.Error(err))
	}

	currentHash, err := fileSHA256(p.cfg.ConfigPath)
	if err != nil {
		return "", err
	}

	keys := make([]string, 0, len(p.profiles))
	for key := range p.profiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sourcePath := p.profiles[key].SourcePath
		if sourcePath == "" {
			continue
		}
		profileHash, err := fileSHA256(sourcePath)
		if err != nil {
			continue
		}
		if bytes.Equal(currentHash[:], profileHash[:]) {
			return key, nil
		}
	}
	return "", nil
}

func (p *Plugin) resolveProfiles(ctx context.Context) (map[string]Profile, error) {
	profiles := normalizeConfiguredProfiles(p.cfg.Profiles)

	configs, err := p.listConfigsWithStartup(ctx)
	if err != nil {
		if len(profiles) == 0 {
			return nil, fmt.Errorf("discover FanControl profiles: %w", err)
		}
		p.logger.Warn("FanControl profile discovery failed; using configured profiles", zap.Error(err))
		return profiles, nil
	}
	mergeDiscoveredProfiles(profiles, configs)
	return profiles, nil
}

func (p *Plugin) listConfigsWithStartup(ctx context.Context) (fanControlConfigs, error) {
	conn, err := openFanControlIPC(ctx)
	if err == nil {
		defer conn.Close()
		return newFanControlRPC(conn).ListConfigs(ctx)
	}
	if !errors.Is(err, errFanControlIPCUnavailable) {
		return fanControlConfigs{}, err
	}
	if startErr := p.startFanControl(ctx); startErr != nil {
		return fanControlConfigs{}, fmt.Errorf("%w; start FanControl: %v", err, startErr)
	}
	return p.waitForConfigs(ctx)
}

func (p *Plugin) startFanControlAndLoad(ctx context.Context, configName string) error {
	if err := p.startFanControl(ctx); err != nil {
		return fmt.Errorf("start FanControl: %w", err)
	}
	conn, err := p.waitForIPC(ctx)
	if err != nil {
		return fmt.Errorf("wait for FanControl IPC: %w", err)
	}
	defer conn.Close()
	rpc := newFanControlRPC(conn)
	if err := rpc.LoadConfig(ctx, configName); err != nil {
		return fmt.Errorf("switch FanControl profile through IPC after start: %w", err)
	}
	return nil
}

func (p *Plugin) startFanControl(ctx context.Context) error {
	running, err := processRunning(ctx, "FanControl.exe")
	if err != nil {
		p.logger.Debug("could not query FanControl process state", zap.Error(err))
	}
	if running {
		return nil
	}
	return startProcess(ctx, p.cfg.ExePath)
}

func (p *Plugin) waitForConfigs(ctx context.Context) (fanControlConfigs, error) {
	conn, err := p.waitForIPC(ctx)
	if err != nil {
		return fanControlConfigs{}, err
	}
	defer conn.Close()
	return newFanControlRPC(conn).ListConfigs(ctx)
}

func (p *Plugin) waitForIPC(ctx context.Context) (*fanControlIPCConn, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := openFanControlIPC(ctx)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, errFanControlIPCUnavailable) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func normalizeConfiguredProfiles(configured map[string]Profile) map[string]Profile {
	profiles := make(map[string]Profile, len(configured))
	for key, profile := range configured {
		if profile.ConfigName == "" && profile.SourcePath != "" {
			profile.ConfigName = filepath.Base(profile.SourcePath)
		}
		if profile.Name == "" {
			profile.Name = friendlyProfileName(profile.configName(key))
		}
		profiles[key] = profile
	}
	return profiles
}

func mergeDiscoveredProfiles(profiles map[string]Profile, configs fanControlConfigs) {
	for _, configName := range configs.Configs {
		configName = filepath.Base(strings.TrimSpace(configName))
		if configName == "" {
			continue
		}
		if key := profileKeyByConfigName(profiles, configName); key != "" {
			profile := profiles[key]
			if profile.ConfigName == "" {
				profile.ConfigName = configName
			}
			if profile.SourcePath == "" && configs.ConfigFolder != "" {
				profile.SourcePath = filepath.Join(configs.ConfigFolder, configName)
			}
			if profile.Name == "" {
				profile.Name = friendlyProfileName(configName)
			}
			profiles[key] = profile
			continue
		}

		key := uniqueProfileKey(profiles, configName)
		profiles[key] = Profile{
			Name:       friendlyProfileName(configName),
			ConfigName: configName,
			SourcePath: sourcePathForConfig(configs.ConfigFolder, configName),
		}
	}
}

func sourcePathForConfig(configFolder, configName string) string {
	if configFolder == "" {
		return ""
	}
	return filepath.Join(configFolder, configName)
}

func profileKeyByConfigName(profiles map[string]Profile, configName string) string {
	discoveredKey := profileKeyFromConfigName(configName)
	for key, profile := range profiles {
		if strings.EqualFold(key, discoveredKey) {
			return key
		}
		if strings.EqualFold(profile.configName(key), configName) {
			return key
		}
	}
	return ""
}

func uniqueProfileKey(profiles map[string]Profile, configName string) string {
	base := profileKeyFromConfigName(configName)
	key := base
	for {
		if _, ok := profiles[key]; !ok {
			return key
		}
		suffix := shortConfigHash(configName)
		key = base + "_" + suffix
		if _, ok := profiles[key]; !ok {
			return key
		}
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s_%s_%d", base, suffix, i)
			if _, ok := profiles[candidate]; !ok {
				return candidate
			}
		}
	}
}

func profileKeyFromConfigName(configName string) string {
	stem := strings.TrimSuffix(filepath.Base(configName), filepath.Ext(configName))
	key := strings.ToLower(stem)
	key = profileKeyUnsafe.ReplaceAllString(key, "_")
	key = strings.Trim(key, "_")
	if key == "" {
		return "profile_" + shortConfigHash(configName)
	}
	return key
}

func shortConfigHash(configName string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(configName)))
	return fmt.Sprintf("%x", sum[:4])
}

func friendlyProfileName(configName string) string {
	stem := strings.TrimSuffix(filepath.Base(configName), filepath.Ext(configName))
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return "FanControl Profile"
	}

	var words []string
	for _, raw := range strings.Fields(profileNameSeparator.ReplaceAllString(stem, " ")) {
		words = append(words, splitCamelWord(raw)...)
	}
	for i, word := range words {
		words[i] = titleASCII(word)
	}
	if len(words) == 0 {
		return stem
	}
	return strings.Join(words, " ")
}

func splitCamelWord(word string) []string {
	if word == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 1; i < len(word); i++ {
		prev := word[i-1]
		cur := word[i]
		if isLowerASCII(prev) && isUpperASCII(cur) {
			out = append(out, word[start:i])
			start = i
		}
	}
	out = append(out, word[start:])
	return out
}

func titleASCII(word string) string {
	if word == "" {
		return word
	}
	lower := strings.ToLower(word)
	first := lower[0]
	if first >= 'a' && first <= 'z' {
		return string(first-'a'+'A') + lower[1:]
	}
	return lower
}

func isLowerASCII(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isUpperASCII(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func (p Profile) configName(key string) string {
	if p.ConfigName != "" {
		return filepath.Base(p.ConfigName)
	}
	if p.SourcePath != "" {
		return filepath.Base(p.SourcePath)
	}
	return key
}

func (p *Plugin) profileByConfigName(configName string) string {
	return profileKeyByConfigName(p.profiles, filepath.Base(configName))
}

func (p *Plugin) timeout() time.Duration {
	if p.cfg.Timeout != 0 {
		return p.cfg.Timeout
	}
	return 20 * time.Second
}

func fileSHA256(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], hash.Sum(nil))
	return sum, nil
}

func processRunning(ctx context.Context, imageName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tasklist.exe", "/FI", "IMAGENAME eq "+imageName, "/NH")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("query process %s: %w: %s", imageName, err, strings.TrimSpace(string(output)))
	}
	return strings.Contains(strings.ToLower(string(output)), strings.ToLower(imageName)), nil
}

func startProcess(ctx context.Context, exePath string) error {
	cmd := exec.CommandContext(ctx, exePath)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (p *Plugin) options() []entity.Option {
	keys := make([]string, 0, len(p.profiles))
	for key := range p.profiles {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := strings.ToLower(p.profiles[keys[i]].Name)
		right := strings.ToLower(p.profiles[keys[j]].Name)
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})

	options := make([]entity.Option, 0, len(keys))
	for _, key := range keys {
		profile := p.profiles[key]
		options = append(options, entity.Option{Value: key, Name: profile.Name})
	}
	return options
}
