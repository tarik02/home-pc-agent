# home-pc-agent

`home-pc-agent` is a Home Assistant PC control agent for Windows and Linux desktop workstations. It exposes selected local PC controls through MQTT while keeping plugins, core state, and transports decoupled.

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

Config validation rejects unknown plugins, unsupported enabled plugins on the current OS, and conflicting providers for the same entity.

## Quick Start

Pick the example config for your platform:

- Linux: `configs/home-pc-agent.linux.example.toml`
- Windows: `configs/home-pc-agent.windows.example.toml`

Validate it:

```bash
go run ./cmd/home-pc-agent config validate --config ./configs/home-pc-agent.linux.example.toml
```

List builtin plugins and configured status:

```bash
go run ./cmd/home-pc-agent plugins list --config ./configs/home-pc-agent.linux.example.toml
```

Run the agent:

```bash
go run ./cmd/home-pc-agent run --config ./configs/home-pc-agent.linux.example.toml
```

The examples keep MQTT disabled so validation works on a fresh machine. Enable MQTT after setting `broker`, `client_id`, and `topic_prefix`.

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

Provider plugins use the `<capability>_<provider>` ID format. Enable one provider per entity on each host.

| Plugin | Platform | Entity |
| --- | --- | --- |
| `session_windows` | Windows | `session.lock`, `session.sleep` |
| `session_loginctl` | Linux | `session.lock`, `session.sleep` |
| `display_windows` | Windows | `display.off` |
| `display_kscreen` | Linux | `display.off` |
| `powerplan_windows` | Windows | `powerplan.mode` |
| `powerprofile_powerprofilesctl` | Linux | `powerprofile.profile` |
| `fancontrol_windows` | Windows | `fancontrol.profile` |
| `runner` | cross-platform | configured actions |

See [docs/plugins/](docs/plugins/) for per-plugin configuration and behavior.

## Service Management

Install and manage the agent as a service on your platform:

```bash
home-pc-agent service install --config /etc/home-pc-agent/home-pc-agent.toml
home-pc-agent service start
home-pc-agent service stop
home-pc-agent service uninstall
```

On Windows this uses the Windows Service Manager. On Linux it writes a user systemd unit under `~/.config/systemd/user/`.

## Release Flow

Development lands through pull requests into `master`. PR titles must follow Conventional Commits because squash merges use the PR title as the commit subject, and Release Please derives changelog entries and SemVer bumps from those commit subjects.

On every `master` push, Release Please creates or updates a release PR with `CHANGELOG.md` entries since the latest stable release. Each non-release `master` push also publishes an immutable prerelease nightly using a `v0.0.0-nightly.<run>.<sha>` tag. Merging the Release Please PR creates the stable GitHub release and uploads GoReleaser artifacts.

Set `RELEASE_PLEASE_TOKEN` to a PAT if release-please-created PRs need to trigger other workflows. Without it, the workflow falls back to `GITHUB_TOKEN`.
