# home-pc-agent

`home-pc-agent` is a Windows-first Home Assistant PC control agent. It exposes selected local PC controls through MQTT while keeping plugins, core state, and transports decoupled.

## Architecture

The core owns the shared runtime primitives:

- Entity Registry: generic entity definitions such as `select`, `button`, `switch`, and `sensor`.
- Event Bus: generic lifecycle, state, and availability events.
- Command Router: typed allowlisted commands keyed by entity ID.
- Plugin Manager: starts builtin Go plugins and tears down their scoped runtime state.
- Transport Manager: starts transports that consume generic entity/events and submit generic commands.
- Scoped Plugin Host: lets plugins register entities, publish state, subscribe commands, and start goroutines through `Host.Go`.

Plugins do not import MQTT packages. MQTT does not import concrete plugins. The v1 transport is MQTT, but the transport host only depends on generic entities, events, and commands so WebSocket or MCP can be added later.

All plugins are builtin Go code. There is no dynamic DLL, executable, or plugin loading.

## Safety Model

`home-pc-agent` does not execute arbitrary shell commands from MQTT. Commands are only accepted for registered entity IDs, and plugins handle typed allowlisted operations. Where an external executable is needed, the code uses `exec.CommandContext` with an explicit executable and argument list.

FanControl profile switching uses FanControl's own named-pipe IPC when it is available:

1. read `FanControlRPC.ListAvailableConfigs` on `\\.\pipe\FanControl` to discover available configs;
2. call `FanControlRPC.LoadConfig` with the selected config filename;
3. keep `FanControl.exe` running during normal switches.

Manual `[plugins.fancontrol.profiles.*]` entries are optional overrides. They can rename discovered configs. If the IPC pipe is unavailable, the plugin tries to start `FanControl.exe` from the configured or default path and waits for IPC. It does not overwrite FanControl config files.

Config reload and plugin unload are supported by the core cleanup model: plugin goroutines are cancelled, command handlers are unsubscribed, entities are unregistered, and transports receive entity removal events so retained discovery configs can be cleared.

## Configuration

Configuration is TOML. Start from:

```powershell
configs\home-pc-agent.example.toml
```

Validate it:

```powershell
go run ./cmd/home-pc-agent config validate --config ./configs/home-pc-agent.example.toml
```

List builtin plugins and configured status:

```powershell
go run ./cmd/home-pc-agent plugins list --config ./configs/home-pc-agent.example.toml
```

Run the agent:

```powershell
go run ./cmd/home-pc-agent run --config ./configs/home-pc-agent.example.toml
```

The example keeps MQTT and FanControl disabled so validation works on a fresh machine. Enable MQTT after setting `broker`, `client_id`, and `topic_prefix`. FanControl defaults to `C:\Program Files (x86)\FanControl\FanControl.exe` and `C:\Program Files (x86)\FanControl\Configurations\userConfig.json`; override `exe_path` or `config_path` only if your install is elsewhere.

Power plans are discovered from Windows automatically:

```toml
[plugins.powerplan]
enabled = true
```

Manual `[plugins.powerplan.modes.*]` entries are optional overrides. They can rename or pin specific GUIDs, and discovered OS schemes are merged by GUID.

## MQTT

MQTT uses retained Home Assistant discovery configs and JSON payloads.

Default topic layout:

- status: `pc/desktop/status`
- state: `pc/desktop/state/<entity_id>`
- availability: `pc/desktop/availability/<entity_id>`
- command: `pc/desktop/command/<entity_id>`
- discovery: `homeassistant/<component>/<object_id>/config`

`topic_prefix` and `discovery_base` are configurable.

## Builtin Plugins

- `powerplan`: exposes `select` entity `powerplan.mode`, discovers Windows schemes with the native Power API when enabled, and applies modes with `powercfg.exe /S <guid>`.
- `fancontrol`: exposes `select` entity `fancontrol.profile`, discovers FanControl configs through IPC when enabled, and switches them through IPC.
- `session`: exposes `button` entities `session.lock`, `session.sleep`, and `session.display_off`.

LibreHardwareMonitor telemetry, WebSocket transport, MCP transport, external plugins, and arbitrary command execution are out of scope for v1.

## Windows Service

The service command group is scaffolded and wired to Windows Service Manager APIs:

```powershell
home-pc-agent service install --config C:\ProgramData\home-pc-agent\home-pc-agent.toml
home-pc-agent service start
home-pc-agent service stop
home-pc-agent service uninstall
```

Service management usually requires an elevated shell.

## Release Flow

Development lands through pull requests into `master`. PR titles must follow Conventional Commits because squash merges use the PR title as the commit subject, and Release Please derives changelog entries and SemVer bumps from those commit subjects.

On every `master` push, Release Please creates or updates a release PR with `CHANGELOG.md` entries since the latest stable release. Each non-release `master` push also publishes an immutable prerelease nightly using a `v0.0.0-nightly.<run>.<sha>` tag. Merging the Release Please PR creates the stable GitHub release and uploads GoReleaser artifacts.

Set `RELEASE_PLEASE_TOKEN` to a PAT if release-please-created PRs need to trigger other workflows. Without it, the workflow falls back to `GITHUB_TOKEN`.
