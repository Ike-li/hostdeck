# hostdeck

A small console for your own computer, on macOS and Windows. See and manage the services, ports, containers and background jobs running on this machine, open their web UIs full-page in one place, and get told when something changes. One small binary, extended with plugins.

[简体中文](README.zh-CN.md)

> **Status: design stage.** There is nothing to install yet. This README records what we are building, and what we are deliberately not building.

## What it will be

- **One place for this machine.** A sidebar of your local web UIs (Grafana, a self-hosted tool on `localhost:3000`, …) that open full-page, and an overview of status cards.
- **Embeds services that refuse to be embedded.** Many tools send `X-Frame-Options` or a CSP `frame-ancestors` to forbid framing. hostdeck serves each one again on its own local port with only that restriction relaxed, and only for the console itself. No per-service configuration.
- **Knows the machine it runs on.** Built-in views of launchd jobs or Windows services, Docker containers, and which process listens on which port, with restart actions.
- **Tells you when something changes.** A card turning yellow or red, or recovering, is pushed to a command, a webhook or a system notification, once the change has been seen twice in a row.
- **Plugins, not a catalogue.** Every card is a plugin: a few lines of YAML for most, an external program for the rest.
- **Small.** A single Go binary serving a plain web UI. It runs in your browser; a thin desktop shell comes later.

## Not planned

- **Access from other devices** (LAN, phone), login, HTTPS. hostdeck listens on 127.0.0.1 only, and its security model depends on that.
- **Linux** before v1, a Docker image, multiple users.
- **A plugin marketplace**, or installing plugins from the network.
- **Hundreds of built-in integrations.** Common services are covered by example plugins.
- **Editing configuration in the web UI** before v1.
- **Electron.**

## Roadmap

Each milestone is released on its own.

### M0 Base

- A local server for macOS and Windows that only this machine can reach
- UI in English and 简体中文, switchable
- A sidebar of your local web UIs that open full-page, including services that forbid being embedded
- An overview of status cards
- Plugin manifest v1: a plugin is a folder whose manifest declares its page, card, detail page and actions
- Declarative plugins: a URL, which fields of its JSON go on the card, and thresholds for yellow and red
- External-program plugins: any executable that prints the card as JSON
- Detail pages that list items, with actions that need a second click to confirm
- Example plugins for a few common services
- CI on macOS and Windows, and release archives for both

### M1 This machine

- launchd jobs on macOS and Windows services, with restart
- Which process listens on which port
- Docker containers, with restart

### M2 Notifications

- A change counts once two checks in a row have seen it, recovery included
- Delivered through a command, a webhook or the system's notifications
- What was last sent is remembered, so a restart does not send it again

### M3 Desktop

- A desktop window showing the same pages as the browser
- A tray icon
- Start at login on macOS and Windows

### M4 Script plugins

- Starlark plugins, for cards that need more than a few lines of YAML
- Network and command access only when the plugin's manifest declares it

## Platforms

macOS and Windows. Windows is **experimental** until it has been tried by hand; for now only CI covers it.

## License

[MIT](LICENSE)
