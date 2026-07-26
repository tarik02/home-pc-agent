# inhibit_freedesktop

Prevent automatic session locking and screen dimming through freedesktop D-Bus APIs.

## Platform

Linux only. Targets desktop sessions that provide the freedesktop ScreenSaver and PowerManagement inhibitor APIs.

## Entities

- `session.lock_inhibited` keeps the desktop from locking automatically.
- `display.dim_inhibited` keeps the desktop from dimming or turning off displays automatically.

Both switches start off when the agent starts. Active inhibitors are released when the agent stops or disconnects from the session bus.

## Configuration

```toml
[plugins.inhibit_freedesktop]
enabled = true
```

## Requirements

- `org.freedesktop.ScreenSaver` on the user session bus
- `org.freedesktop.PowerManagement.Inhibit` on the user session bus

The agent must run in the desktop user's session. The Linux systemd user service provides the required session bus access.
