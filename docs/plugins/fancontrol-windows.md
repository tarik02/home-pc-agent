# fancontrol_windows

FanControl profile switching through FanControl IPC.

## Platform

Windows only.

## Entities

- `fancontrol.profile` — select entity for active FanControl config

## Configuration

```toml
[plugins.fancontrol_windows]
enabled = true
timeout = "20s"
# exe_path = 'C:\Program Files (x86)\FanControl\FanControl.exe'
# config_path = 'C:\Program Files (x86)\FanControl\Configurations\userConfig.json'
```

Manual profile overrides are optional:

```toml
[plugins.fancontrol_windows.profiles.gaming]
name = "Gaming"
config_name = "gaming.json"
```

## Behavior

When FanControl IPC is available:

1. read `FanControlRPC.ListAvailableConfigs` on `\\.\pipe\FanControl`
2. call `FanControlRPC.LoadConfig` with the selected config filename
3. keep `FanControl.exe` running during normal switches

If the IPC pipe is unavailable, the plugin tries to start `FanControl.exe` and waits for IPC. It does not overwrite FanControl config files.

Linux fan profile control is out of scope.
