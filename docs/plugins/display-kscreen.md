# display_kscreen

Turn off displays on KDE through `kscreen-doctor`.

## Platform

Linux only. Targets KDE Wayland workstations.

## Entities

- `display.off` — `kscreen-doctor --dpms off`

## Configuration

```toml
[plugins.display_kscreen]
enabled = true
```

## Requirements

- `kscreen-doctor` on `PATH`

X11 and GNOME portal backends are out of scope for this plugin.
