package powerprofile_ppd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

const (
	pluginID            = "powerprofile_ppd"
	entityID            = "powerprofile.profile"
	retryInterval       = 5 * time.Second
	propertiesInterface = "org.freedesktop.DBus.Properties"
	nameOwnerChanged    = "org.freedesktop.DBus.NameOwnerChanged"
	propertiesChanged   = propertiesInterface + ".PropertiesChanged"
)

type service struct {
	name          string
	path          dbus.ObjectPath
	interfaceName string
}

// power-profiles-daemon 0.20 renamed its API; the legacy name keeps older
// releases (Debian 12, Ubuntu 22.04) working.
var services = []*service{
	{
		name:          "org.freedesktop.UPower.PowerProfiles",
		path:          dbus.ObjectPath("/org/freedesktop/UPower/PowerProfiles"),
		interfaceName: "org.freedesktop.UPower.PowerProfiles",
	},
	{
		name:          "net.hadess.PowerProfiles",
		path:          dbus.ObjectPath("/net/hadess/PowerProfiles"),
		interfaceName: "net.hadess.PowerProfiles",
	},
}

// The daemon always offers these two, so they stand in for the real list
// until it becomes reachable.
var placeholderProfiles = []string{"power-saver", "balanced"}

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

type snapshot struct {
	profiles []string
	active   string
}

type Plugin struct {
	cfg    Config
	host   plugin.PluginHost
	logger *zap.Logger
	conn   *dbus.Conn

	mu sync.Mutex
	// service is the daemon API currently in use; nil while it is unreachable.
	service *service
	options []entity.Option
	active  string
}

func (p *Plugin) Start(ctx context.Context, host plugin.PluginHost) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect to system bus: %w", err)
	}
	started := false
	defer func() {
		if !started {
			_ = conn.Close()
		}
	}()

	for _, svc := range services {
		if err := conn.AddMatchSignalContext(ctx,
			dbus.WithMatchSender("org.freedesktop.DBus"),
			dbus.WithMatchInterface("org.freedesktop.DBus"),
			dbus.WithMatchMember("NameOwnerChanged"),
			dbus.WithMatchArg(0, svc.name),
		); err != nil {
			return fmt.Errorf("watch %s owner: %w", svc.name, err)
		}
		if err := conn.AddMatchSignalContext(ctx,
			dbus.WithMatchObjectPath(svc.path),
			dbus.WithMatchInterface(propertiesInterface),
			dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchArg(0, svc.interfaceName),
		); err != nil {
			return fmt.Errorf("watch %s properties: %w", svc.name, err)
		}
	}
	// Buffered because godbus blocks its reader while delivering a signal, and
	// the watcher itself makes D-Bus calls while handling one.
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)

	p.host = host
	p.conn = conn

	svc, snap, loadErr := p.load(ctx)
	profiles := placeholderProfiles
	if loadErr == nil {
		profiles = snap.profiles
	}
	p.options = profileOptions(profiles)
	if err := host.RegisterEntity(p.entity()); err != nil {
		return err
	}
	if err := host.SubscribeCommand(entityID, p.handleCommand); err != nil {
		return err
	}

	if loadErr == nil {
		p.mu.Lock()
		p.applySnapshotLocked(svc, snap)
		p.mu.Unlock()
	} else {
		_ = host.SetAvailability(entityID, entity.AvailabilityUnavailable)
		p.logger.Warn("power-profiles-daemon unavailable; retrying", zap.Error(loadErr))
	}

	host.Go("ppd-signals", func(loopCtx context.Context) error {
		return p.watch(loopCtx, signals)
	})

	started = true
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	// Closing the connection also closes the signal channel, ending the watcher.
	if err := p.conn.Close(); err != nil {
		return fmt.Errorf("close system bus: %w", err)
	}
	return nil
}

func (p *Plugin) entity() entity.Entity {
	return entity.Entity{
		ID:      entityID,
		Name:    "Power Profile",
		Kind:    entity.KindSelect,
		Options: p.options,
		Icon:    "mdi:power-plug",
	}
}

func (p *Plugin) watch(ctx context.Context, signals <-chan *dbus.Signal) error {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case signal, ok := <-signals:
			if !ok {
				return nil
			}
			p.handleSignal(ctx, signal)
		case <-ticker.C:
			p.mu.Lock()
			available := p.service != nil
			p.mu.Unlock()
			if !available {
				p.reload(ctx)
			}
		}
	}
}

func (p *Plugin) handleSignal(ctx context.Context, signal *dbus.Signal) {
	switch signal.Name {
	case nameOwnerChanged:
		var name, oldOwner, newOwner string
		if err := dbus.Store(signal.Body, &name, &oldOwner, &newOwner); err != nil {
			p.logger.Debug("ignoring malformed NameOwnerChanged signal", zap.Error(err))
			return
		}
		if !slices.ContainsFunc(services, func(svc *service) bool { return svc.name == name }) {
			return
		}
		p.logger.Debug("power-profiles-daemon owner changed", zap.String("name", name), zap.String("owner", newOwner))
		p.reload(ctx)

	case propertiesChanged:
		var interfaceName string
		var changed map[string]dbus.Variant
		var invalidated []string
		if err := dbus.Store(signal.Body, &interfaceName, &changed, &invalidated); err != nil {
			p.logger.Debug("ignoring malformed PropertiesChanged signal", zap.Error(err))
			return
		}
		if slices.Contains(invalidated, "ActiveProfile") || slices.Contains(invalidated, "Profiles") {
			p.reload(ctx)
			return
		}
		p.applyChanges(signal.Path, interfaceName, changed)
	}
}

// load reads the daemon state from the first API that answers.
func (p *Plugin) load(ctx context.Context) (*service, snapshot, error) {
	var errs error
	for _, svc := range services {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		var props map[string]dbus.Variant
		// No auto-start: the retry loop would otherwise keep trying to
		// activate a daemon that is masked or not installed.
		err := p.conn.Object(svc.name, svc.path).
			CallWithContext(callCtx, propertiesInterface+".GetAll", dbus.FlagNoAutoStart, svc.interfaceName).
			Store(&props)
		cancel()
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", svc.name, err))
			continue
		}
		snap, err := parseSnapshot(props)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", svc.name, err))
			continue
		}
		return svc, snap, nil
	}
	return nil, snapshot{}, errs
}

func (p *Plugin) reload(ctx context.Context) {
	svc, snap, err := p.load(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if p.service != nil {
			p.service = nil
			_ = p.host.SetAvailability(entityID, entity.AvailabilityUnavailable)
			p.logger.Warn("power-profiles-daemon unavailable; retrying", zap.Error(err))
		}
		return
	}
	p.applySnapshotLocked(svc, snap)
}

func (p *Plugin) applySnapshotLocked(svc *service, snap snapshot) {
	wasAvailable := p.service != nil
	p.service = svc
	if !p.setProfilesLocked(snap.profiles) {
		_ = p.host.SetAvailability(entityID, entity.AvailabilityOnline)
	}
	p.publishActiveLocked(snap.active)
	if !wasAvailable {
		p.logger.Info("power-profiles-daemon available",
			zap.String("service", svc.name),
			zap.Strings("profiles", snap.profiles),
			zap.String("active", snap.active),
		)
	}
}

func (p *Plugin) applyChanges(path dbus.ObjectPath, interfaceName string, changed map[string]dbus.Variant) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.service == nil || p.service.path != path || p.service.interfaceName != interfaceName {
		return
	}

	profilesUpdated := false
	if value, ok := changed["Profiles"]; ok {
		profiles, err := parseProfiles(value)
		if err != nil {
			p.logger.Warn("ignoring invalid power profile list", zap.Error(err))
		} else {
			profilesUpdated = p.setProfilesLocked(profiles)
		}
	}

	if value, ok := changed["ActiveProfile"]; ok {
		active, ok := value.Value().(string)
		if !ok || active == "" {
			p.logger.Warn("ignoring invalid active power profile", zap.String("signature", value.Signature().String()))
		} else {
			if active != p.active {
				p.logger.Info("power profile changed", zap.String("profile", active))
			}
			p.publishActiveLocked(active)
			return
		}
	}

	// Updating the entity republishes its discovery, so restate the profile.
	if profilesUpdated {
		p.publishActiveLocked(p.active)
	}
}

// setProfilesLocked updates the select options and reports whether the entity
// had to be updated, which also marks it online.
func (p *Plugin) setProfilesLocked(profiles []string) bool {
	options := profileOptions(profiles)
	if slices.Equal(options, p.options) {
		return false
	}
	p.options = options
	if err := p.host.UpdateEntity(p.entity()); err != nil {
		p.logger.Warn("update power profile options failed", zap.Error(err))
		return false
	}
	p.logger.Info("power profiles changed", zap.Strings("profiles", profiles))
	_ = p.host.SetAvailability(entityID, entity.AvailabilityOnline)
	return true
}

func (p *Plugin) publishActiveLocked(active string) {
	p.active = active
	if active == "" {
		return
	}
	_ = p.host.PublishState(entityID, entity.OptionName(p.options, active))
}

func (p *Plugin) handleCommand(ctx context.Context, command plugin.Command) error {
	value, ok := command.Payload.(string)
	if !ok || value == "" {
		return fmt.Errorf("powerprofile command payload must be a string option")
	}

	p.mu.Lock()
	svc := p.service
	profileID, known := entity.OptionValue(p.options, value)
	p.mu.Unlock()
	if svc == nil {
		return fmt.Errorf("power-profiles-daemon is not available")
	}
	if !known {
		return fmt.Errorf("unknown power profile option %q", value)
	}

	callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	setErr := p.conn.Object(svc.name, svc.path).
		CallWithContext(callCtx, propertiesInterface+".Set", 0, svc.interfaceName, "ActiveProfile", dbus.MakeVariant(profileID)).
		Err

	// Publish what the daemon actually applied: a rejected switch leaves the old
	// profile active, and an unchanged profile emits no PropertiesChanged.
	var active string
	readErr := p.conn.Object(svc.name, svc.path).
		CallWithContext(callCtx, propertiesInterface+".Get", 0, svc.interfaceName, "ActiveProfile").
		Store(&active)
	if readErr == nil {
		p.mu.Lock()
		if p.service == svc {
			p.publishActiveLocked(active)
		}
		p.mu.Unlock()
	}

	if setErr != nil {
		return fmt.Errorf("set power profile %q: %w", profileID, setErr)
	}
	if readErr != nil {
		return fmt.Errorf("read active power profile: %w", readErr)
	}
	return nil
}

func parseSnapshot(props map[string]dbus.Variant) (snapshot, error) {
	value, ok := props["Profiles"]
	if !ok {
		return snapshot{}, fmt.Errorf("property Profiles is missing")
	}
	profiles, err := parseProfiles(value)
	if err != nil {
		return snapshot{}, err
	}
	active, ok := props["ActiveProfile"].Value().(string)
	if !ok {
		return snapshot{}, fmt.Errorf("property ActiveProfile is missing")
	}
	return snapshot{profiles: profiles, active: active}, nil
}

// parseProfiles extracts profile IDs from the aa{sv} Profiles property; each
// entry also carries driver details that the agent does not need.
func parseProfiles(value dbus.Variant) ([]string, error) {
	entries, ok := value.Value().([]map[string]dbus.Variant)
	if !ok {
		return nil, fmt.Errorf("property Profiles has unexpected signature %s", value.Signature())
	}
	profiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		id, ok := entry["Profile"].Value().(string)
		if !ok || id == "" || slices.Contains(profiles, id) {
			continue
		}
		profiles = append(profiles, id)
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no power profiles available")
	}
	return profiles, nil
}

func profileOptions(profiles []string) []entity.Option {
	options := make([]entity.Option, 0, len(profiles))
	for _, id := range profiles {
		options = append(options, entity.Option{Value: id, Name: friendlyProfileName(id)})
	}
	sort.Slice(options, func(i, j int) bool {
		left := strings.ToLower(options[i].Name)
		right := strings.ToLower(options[j].Name)
		if left == right {
			return options[i].Value < options[j].Value
		}
		return left < right
	})
	return options
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
