# session_loginctl

Linux session controls through `loginctl` and `systemctl`.

## Platform

Linux only.

## Entities

- `session.lock` — `loginctl lock-session`
- `session.sleep` — `systemctl suspend`

## Configuration

```toml
[plugins.session_loginctl]
enabled = true
```

## Requirements

- `loginctl` on `PATH`
- `systemctl` with suspend support

If required commands are missing, the plugin fails at start.
