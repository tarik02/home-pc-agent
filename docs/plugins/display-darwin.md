# display_darwin

Turn off displays on macOS through the system power manager.

## Platform

macOS only.

## Entities

- `display.off` — `pmset displaysleepnow`

## Configuration

```toml
[plugins.display_darwin]
enabled = true
```

## Requirements

- `/usr/bin/pmset`
