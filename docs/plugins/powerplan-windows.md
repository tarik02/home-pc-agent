# powerplan_windows

Windows power scheme selection through `powercfg.exe`.

## Platform

Windows only.

## Entities

- `powerplan.mode` — select entity for active power plan

## Configuration

```toml
[plugins.powerplan_windows]
enabled = true
timeout = "10s"
```

Power plans are discovered from Windows automatically. Optional overrides:

```toml
[plugins.powerplan_windows.modes.balanced]
name = "Balanced"
guid = "381b4222-f694-41f0-9685-ff5bb260df2e"
```

Discovered OS schemes are merged by GUID.

## Behavior

- Active plan: `powercfg.exe /GETACTIVESCHEME`
- Apply plan: `powercfg.exe /S <guid>`
