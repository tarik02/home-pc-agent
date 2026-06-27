# runner

Allowlisted script execution for custom entities.

## Platform

Cross-platform.

## Configuration

```toml
[plugins.runner]
enabled = true
default_timeout = "120s"

[plugins.runner.actions.display_mode]
name = "Display Mode"
kind = "select"
icon = "mdi:monitor"

[plugins.runner.actions.display_mode.state]
source = "none"

[plugins.runner.actions.display_mode.setter]
path = "/var/lib/home-pc-agent/scripts/modeswitch.sh"
interpreter = "bash"
input = "structured"
delivery = "args"

[plugins.runner.actions.display_mode.options.default]
name = "Default"
parameters = { mode = "default" }
```

## Supported interpreters

- `pwsh`, `powershell`, `powershell.exe`
- `cmd`
- `exe`
- `sh`
- `bash`

Scripts and executables must be declared in config. MQTT cannot invoke arbitrary commands.

## Entity kinds

- `button`
- `select`
- `switch`

Entity IDs default to `runner.<action_key>` unless `entity_id` is set.

## Windows example

```toml
[plugins.runner.actions.display_mode.setter]
path = "C:\\ProgramData\\home-pc-agent\\scripts\\modeswitch.ps1"
interpreter = "pwsh"
```

## Linux example

```toml
[plugins.runner.actions.display_mode.setter]
path = "/var/lib/home-pc-agent/scripts/modeswitch.sh"
interpreter = "bash"
```
