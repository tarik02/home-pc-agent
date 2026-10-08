# powerprofile_ppd

Linux power profile selection through power-profiles-daemon over the system D-Bus.

## Platform

Linux only.

## Entities

- `powerprofile.profile` — select entity for active power profile

## Configuration

```toml
[plugins.powerprofile_ppd]
enabled = true
timeout = "10s"
```

`timeout` bounds each D-Bus call to the daemon.

## Behavior

- Profiles come from the daemon's `Profiles` property and are shown as `Power Saver`, `Balanced`, and `Performance`.
- Selecting an option writes the `ActiveProfile` property. If the daemon rejects the switch, the command fails with its error and the entity keeps showing the profile that is actually active.
- Profile changes made elsewhere (desktop settings, `powerprofilesctl`, other tools) are followed through `PropertiesChanged` signals.
- The entity is unavailable while the daemon is not running and recovers when it starts or restarts. The agent does not start the daemon itself.

## Requirements

- power-profiles-daemon on the system bus as `org.freedesktop.UPower.PowerProfiles` (0.20 and later) or `net.hadess.PowerProfiles` (older releases)
- Permission to change the profile: root is allowed by default; other users depend on the polkit policy

## Notes

This is separate from Windows `powerplan.mode`. Do not enable both on the same host.

The plugin was previously named `powerprofile_powerprofilesctl`; rename the `[plugins.powerprofile_powerprofilesctl]` config section to `[plugins.powerprofile_ppd]`.
