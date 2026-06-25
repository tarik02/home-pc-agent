//go:build windows

package fancontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeDiscoveredProfiles(t *testing.T) {
	profiles := map[string]Profile{}
	mergeDiscoveredProfiles(profiles, fanControlConfigs{
		ConfigFolder: `C:\Program Files (x86)\FanControl\Configurations`,
		Configs: []string{
			"terra-optimized.json",
			"terra-gaming.json",
			"userConfig.json",
		},
	})

	require.Equal(t, Profile{
		Name:       "Terra Optimized",
		ConfigName: "terra-optimized.json",
		SourcePath: `C:\Program Files (x86)\FanControl\Configurations\terra-optimized.json`,
	}, profiles["terra_optimized"])
	require.Equal(t, "Terra Gaming", profiles["terra_gaming"].Name)
	require.Equal(t, "User Config", profiles["userconfig"].Name)
}

func TestMergeDiscoveredProfilesKeepsConfiguredOverrides(t *testing.T) {
	profiles := normalizeConfiguredProfiles(map[string]Profile{
		"quiet": {
			Name:       "Silent",
			ConfigName: "terra-quiet.json",
		},
		"terra_optimized": {
			Name: "Optimized Override",
		},
	})
	mergeDiscoveredProfiles(profiles, fanControlConfigs{
		ConfigFolder: `C:\FanControl\Configurations`,
		Configs:      []string{"terra-quiet.json", "terra-max.json", "terra-optimized.json"},
	})

	require.Equal(t, Profile{
		Name:       "Silent",
		ConfigName: "terra-quiet.json",
		SourcePath: `C:\FanControl\Configurations\terra-quiet.json`,
	}, profiles["quiet"])
	require.Equal(t, "Terra Max", profiles["terra_max"].Name)
	require.Equal(t, Profile{
		Name:       "Optimized Override",
		ConfigName: "terra-optimized.json",
		SourcePath: `C:\FanControl\Configurations\terra-optimized.json`,
	}, profiles["terra_optimized"])
}
