# display_windows

Turn off displays on Windows through the monitor power API.

## Platform

Windows only.

## Entities

- `display.off` — broadcast `WM_SYSCOMMAND` / `SC_MONITORPOWER`

## Configuration

```toml
[plugins.display_windows]
enabled = true
```

## Notes

Previously exposed as `session.display_off` on the old `session` plugin.
