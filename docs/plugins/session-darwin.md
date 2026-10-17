# session_darwin

macOS session controls through native system APIs.

## Platform

macOS only.

## Entities

- `session.lock` — lock the current session immediately
- `session.sleep` — `pmset sleepnow`
- `session.locked` — current console lock state, refreshed every 10 seconds

## Configuration

```toml
[plugins.session_darwin]
enabled = true
```

## Requirements

- The system `login` framework
- `/usr/bin/pmset`
- `/usr/sbin/ioreg`
- `/usr/bin/plutil`

The plugin fails at start if the lock-screen API or required commands are unavailable.
