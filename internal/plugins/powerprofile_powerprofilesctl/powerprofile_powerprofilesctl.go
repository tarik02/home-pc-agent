package powerprofile_powerprofilesctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const (
	pluginID = "powerprofile_powerprofilesctl"
	entityID = "powerprofile.profile"
)

var profileLinePattern = regexp.MustCompile(`^\s*\*?\s*(\S+):`)

type Config struct {
	Enabled bool          `mapstructure:"enabled"`
	Timeout time.Duration `mapstructure:"timeout"`
}

func NewFactory() plugin.Factory {
	return plugin.ConfigFactory[Config](
		plugin.Descriptor{
			ID:               pluginID,
			OperatingSystems: []string{"linux"},
			EntityIDs:        []string{entityID},
		},
		func(cfg Config) (Config, error) {
			if cfg.Timeout == 0 {
				cfg.Timeout = 10 * time.Second
			}
			return cfg, nil
		},
		func(cfg Config, logger *zap.Logger) plugin.Plugin {
			return &Plugin{cfg: cfg, logger: logger}
		},
	)
}

type Profile struct {
	ID   string
	Name string
}

type Plugin struct {
	cfg      Config
	profiles map[string]Profile
	host     plugin.PluginHost
	logger   *zap.Logger
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	p.host = host
	profiles, err := p.discoverProfiles(ctx)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no power profiles available")
	}
	p.profiles = profiles
	options := p.options()
	err = host.RegisterEntity(entity.Entity{
		ID:      entityID,
		Name:    "Power Profile",
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
	if active, err := p.activeProfile(ctx); err == nil && active != "" {
		_ = host.PublishState(entityID, entity.OptionName(options, active))
	} else if err != nil {
		p.logger.Debug("active power profile could not be detected", zap.Error(err))
	}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *Plugin) handleCommand(ctx context.Context, command plugin.Command) error {
	value, ok := command.Payload.(string)
	if !ok || value == "" {
		return fmt.Errorf("powerprofile command payload must be a string option")
	}
	profileID, ok := entity.OptionValue(p.options(), value)
	if !ok {
		return fmt.Errorf("unknown power profile option %q", value)
	}
	profile, ok := p.profiles[profileID]
	if !ok {
		return fmt.Errorf("power profile %q is no longer available", profileID)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "powerprofilesctl", "set", profile.ID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = p.host.SetAvailability(entityID, entity.AvailabilityUnavailable)
		return fmt.Errorf("set power profile %q: %w: %s", profileID, err, strings.TrimSpace(stderr.String()))
	}

	_ = p.host.SetAvailability(entityID, entity.AvailabilityOnline)
	return p.host.PublishState(entityID, entity.OptionName(p.options(), profileID))
}

func (p *Plugin) discoverProfiles(ctx context.Context) (map[string]Profile, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	output, err := exec.CommandContext(cmdCtx, "powerprofilesctl", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list power profiles: %w: %s", err, strings.TrimSpace(string(output)))
	}

	profiles := make(map[string]Profile)
	for _, line := range strings.Split(string(output), "\n") {
		matches := profileLinePattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		id := matches[1]
		profiles[id] = Profile{ID: id, Name: friendlyProfileName(id)}
	}
	return profiles, nil
}

func (p *Plugin) activeProfile(ctx context.Context) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	output, err := exec.CommandContext(cmdCtx, "powerprofilesctl", "get").CombinedOutput()
	if err != nil {
		return "", err
	}
	active := strings.TrimSpace(string(output))
	if active == "" {
		return "", nil
	}
	if _, ok := p.profiles[active]; ok {
		return active, nil
	}
	return "", nil
}

func friendlyProfileName(id string) string {
	words := strings.Split(strings.ReplaceAll(id, "-", " "), " ")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
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
