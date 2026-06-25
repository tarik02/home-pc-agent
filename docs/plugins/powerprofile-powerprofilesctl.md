# powerprofile_powerprofilesctl

Linux power profile selection through `powerprofilesctl`.

## Platform

Linux only.

## Entities

- `powerprofile.profile` — select entity for active power profile

## Configuration

```toml
[plugins.powerprofile_powerprofilesctl]
enabled = true
timeout = "10s"
```

Profiles are discovered from `powerprofilesctl list` at plugin start.

## Behavior

- List profiles: `powerprofilesctl list`
- Active profile: `powerprofilesctl get`
- Apply profile: `powerprofilesctl set <profile>`

## Notes

This is separate from Windows `powerplan.mode`. Do not enable both on the same host.
