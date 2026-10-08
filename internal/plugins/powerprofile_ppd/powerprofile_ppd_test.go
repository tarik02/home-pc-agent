package powerprofile_ppd

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

func profileEntry(id string, drivers map[string]string) map[string]dbus.Variant {
	entry := map[string]dbus.Variant{"Profile": dbus.MakeVariant(id)}
	for key, value := range drivers {
		entry[key] = dbus.MakeVariant(value)
	}
	return entry
}

func profilesVariant(entries ...map[string]dbus.Variant) dbus.Variant {
	return dbus.MakeVariant(entries)
}

func TestParseProfilesReadsProfileKeys(t *testing.T) {
	profiles, err := parseProfiles(profilesVariant(
		profileEntry("power-saver", map[string]string{"CpuDriver": "intel_pstate", "PlatformDriver": "placeholder"}),
		profileEntry("balanced", map[string]string{"CpuDriver": "intel_pstate"}),
		profileEntry("performance", map[string]string{"Driver": "intel_pstate"}),
		profileEntry("balanced", nil),
		map[string]dbus.Variant{"CpuDriver": dbus.MakeVariant("amd_pstate")},
	))

	require.NoError(t, err)
	require.Equal(t, []string{"power-saver", "balanced", "performance"}, profiles)
}

func TestParseProfilesRejectsUnexpectedValues(t *testing.T) {
	_, err := parseProfiles(dbus.MakeVariant("balanced"))
	require.Error(t, err)

	_, err = parseProfiles(profilesVariant())
	require.Error(t, err)
}

func TestProfileOptionsKeepHomeAssistantNames(t *testing.T) {
	options := profileOptions([]string{"power-saver", "balanced", "performance"})

	require.Equal(t, []entity.Option{
		{Value: "balanced", Name: "Balanced"},
		{Value: "performance", Name: "Performance"},
		{Value: "power-saver", Name: "Power Saver"},
	}, options)
}

type fakeHost struct {
	entities     []entity.Entity
	states       []any
	availability []entity.Availability
}

func (h *fakeHost) RegisterEntity(e entity.Entity) error {
	h.entities = append(h.entities, e)
	return nil
}

func (h *fakeHost) UpdateEntity(e entity.Entity) error {
	h.entities = append(h.entities, e)
	return nil
}

func (h *fakeHost) PublishState(_ string, state any) error {
	h.states = append(h.states, state)
	return nil
}

func (h *fakeHost) SetAvailability(_ string, availability entity.Availability) error {
	h.availability = append(h.availability, availability)
	return nil
}

func (h *fakeHost) SubscribeCommand(string, plugin.CommandHandler) error { return nil }

func (h *fakeHost) Go(string, func(ctx context.Context) error) {}

func newTestPlugin(host *fakeHost) *Plugin {
	p := &Plugin{host: host, logger: zap.NewNop()}
	p.applySnapshotLocked(services[0], snapshot{profiles: []string{"power-saver", "balanced"}, active: "power-saver"})
	*host = fakeHost{}
	return p
}

func TestApplyChangesPublishesExternalProfileChange(t *testing.T) {
	host := &fakeHost{}
	p := newTestPlugin(host)

	p.applyChanges(services[0].path, services[0].interfaceName, map[string]dbus.Variant{
		"ActiveProfile": dbus.MakeVariant("balanced"),
	})

	require.Equal(t, []any{"Balanced"}, host.states)
	require.Empty(t, host.entities)
}

func TestApplyChangesUpdatesOptionsAndRestatesProfile(t *testing.T) {
	host := &fakeHost{}
	p := newTestPlugin(host)

	p.applyChanges(services[0].path, services[0].interfaceName, map[string]dbus.Variant{
		"Profiles": profilesVariant(
			profileEntry("power-saver", nil),
			profileEntry("balanced", nil),
			profileEntry("performance", nil),
		),
	})

	require.Len(t, host.entities, 1)
	require.Equal(t, []string{"Balanced", "Performance", "Power Saver"}, entity.OptionNames(host.entities[0].Options))
	require.Equal(t, []entity.Availability{entity.AvailabilityOnline}, host.availability)
	require.Equal(t, []any{"Power Saver"}, host.states)
}

func TestApplyChangesIgnoresOtherServices(t *testing.T) {
	host := &fakeHost{}
	p := newTestPlugin(host)

	p.applyChanges(services[1].path, services[1].interfaceName, map[string]dbus.Variant{
		"ActiveProfile": dbus.MakeVariant("balanced"),
	})

	require.Empty(t, host.states)
}
