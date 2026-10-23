# hostdeck

A small console for your own computer, on macOS and Windows. See and manage the services, ports, containers and background jobs running on this machine, open their web UIs full-page in one place, and get told when something changes. One small binary, extended with plugins.

[简体中文](README.zh-CN.md)

> **Status: early, not released.** Most of M0 works and runs from source (see [Run from source](#run-from-source)); there is no release to install yet. This README records what we are building, and what we are deliberately not building.

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
- **Installing plugins from the network**, or a marketplace of third-party plugins. The plugins page adds only the plugins built into hostdeck, plugins that embed a web page on this machine or the local network from its address, and links.
- **Hundreds of built-in integrations.** Common services are covered by example plugins.
- **Editing configuration in the web UI** before v1, apart from adding a built-in plugin, embedding a web page on this machine or the local network from its address, adding a link, changing the name, icon or address of a page, disabling or enabling a plugin, moving one to the trash and purging it from there, and arranging, hiding or resetting the sidebar and the cards.
- **Electron.**

## Roadmap

Each milestone is released on its own.

### M0 Base

- A local server for macOS and Windows that only this machine can reach
- UI in English and 简体中文, switchable
- A sidebar of your local web UIs that open full-page, including services that forbid being embedded
- A clear-cache button on each embedded service that clears that service's cache and site data and nothing else: cookies and other services are left alone
- An overview of status cards
- Sidebar entries and cards dragged into the order you like, and cards made two columns wide; kept in `layout.json`
- A plugin can be hidden from the sidebar and the cards, and the layout can be reset; both can be undone, and both are kept in `layout.json`
- A page's name, icon or address can be edited; comments and commands in the manifest stay, and an embed address is checked again to be on this machine or the local network
- A link to any web page can be added; it is not embedded and its manifest has no command
- A plugin can be moved to the trash and purged from there; restoring it puts the folder back enabled, and its forwarding port stays with its id
- Plugin manifest v1: a plugin is a folder whose manifest declares its page, card, detail page and actions
- Declarative plugins: a URL, which fields of its JSON go on the card, and thresholds for yellow and red
- External-program plugins: any executable that prints the card as JSON
- Detail pages that list items, with actions that need a second click to confirm
- Example plugins for a few common services
- A plugins page that adds the plugins built into hostdeck, without the network, embeds a web page on this machine or the local network from its address or by dragging it in, and disables or enables installed ones
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

## Run from source

hostdeck has no release yet. To try what is there, with Go 1.26 or later:

```sh
git clone https://github.com/Ike-li/hostdeck
cd hostdeck
go run .
```

Then open http://127.0.0.1:4678/.

Plugins are folders under `plugins/` in hostdeck's own folder: `~/.config/hostdeck` on macOS, `%LOCALAPPDATA%\hostdeck` on Windows. `--dir` or `HOSTDECK_DIR` picks another folder, and `--addr` another port on 127.0.0.1. The overview's **+ Add a page or plugin** card opens the add area for web pages, links and built-in plugins. The **Pages & plugins** page in the sidebar separates added connections from adding content, and adds the ones that come with hostdeck, and disables or enables installed ones, each change taking effect at once: a disabled plugin's folder is renamed to start with a dot, which hostdeck skips, and nothing in it changes. Hiding only removes a connection from the overview and sidebar; **Restore to workspace** brings it back. Enabling or disabling does not start or stop its external service. A plugin folder added or changed by hand is read when hostdeck restarts, or with the next change on that page. A web page on this machine or the local network is embedded from the same page by its address, or by dragging the address onto hostdeck; that writes a plugin folder whose `plugin.yaml` holds only `page.proxy`. Dragging a link from within the console, or dropping the console's own address, does not add a page.

A plugin is a folder holding a `plugin.yaml`. Two small ones:

```yaml
# plugins/grafana/plugin.yaml: Grafana in the sidebar, embedded
schema: 1
name: Grafana
page:
  proxy: http://127.0.0.1:3000/
```

```yaml
# plugins/health/plugin.yaml: a card showing one value of a JSON endpoint
schema: 1
name: My service
card:
  http:
    url: http://127.0.0.1:8080/health
  value: status
  label: {en: Status, zh-CN: 状态}
```

A card can also turn yellow or red, list more values in rows, and send a token kept outside the file:

```yaml
# plugins/queue/plugin.yaml
schema: 1
name: Job queue
card:
  interval: 30s          # how long a reading is kept; 1m when left out
  http:
    url: http://127.0.0.1:8080/stats
    headers:
      Authorization: Bearer ${env:QUEUE_TOKEN}
  value: queue.waiting   # a path into the JSON response, in gjson syntax
  label: waiting
  warn: {above: 50}
  error: {above: 200}
  rows:
    - name: Failed today
      value: failed.today
      error: {above: 0}
    - name: Workers
      value: workers.state
      warn: {not: running}
```

- `warn` and `error` each take one of `above`, `below`, `equals` or `not`. When both match, the card is red.
- `${env:NAME}` works in `url` and `headers`. The value is sent to the service, and shows as `***` in any error on the card. hostdeck must be started with the variable set.
- A plugin whose `plugin.yaml` has a mistake shows up as an error card that says what is wrong; the other plugins still load.

Paths follow [gjson](https://github.com/tidwall/gjson/blob/master/SYNTAX.md). [`examples/`](examples) has complete plugins to copy, including one whose card and detail page come from a script.

The manifest format may still change before the first release.

## How it works

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) describes the pieces, what happens on each request, and the security model.

## Platforms

macOS and Windows. Windows is **experimental** until it has been tried by hand; for now only CI covers it.

## License

[MIT](LICENSE)
