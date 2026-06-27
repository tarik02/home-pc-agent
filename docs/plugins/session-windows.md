# session_windows

Windows session controls through Win32 APIs.

## Platform

Windows only.

## Entities

- `session.lock` — lock the workstation with `LockWorkStation`
- `session.sleep` — suspend with `SetSuspendState`

## Configuration

```toml
[plugins.session_windows]
enabled = true
```

## Notes

Display off moved to the `display_windows` plugin.
