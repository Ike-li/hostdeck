# hostdeck

A small console for your own computer, on macOS and Windows. See and manage the services, ports, containers and background jobs running on this machine, open their web UIs full-page in one place, and get told when something changes. One small binary, extended with plugins.

[简体中文](README.zh-CN.md)

> **Status: early, not released.** Most of M0 works and runs from source (see [Run from source](#run-from-source)); there is no release to install yet. This README records what we are building, and what we are deliberately not building.

## What it will be

- **One place for this machine.** A sidebar of your local web UIs (Grafana, a self-hosted tool on `localhost:3000`, …) that open full-page, and an overview of status cards.
- **Embeds services that refuse to be embedded.** Many tools send `X-Frame-Options` or a CSP `frame-ancestors` to forbid framing. hostdeck serves each one again on its own local port with only that restriction relaxed, and only for the console itself. No per-service configuration.
- **Knows the machine it runs on.** Built-in views of launchd jobs or Windows services, Docker containers, and which process listens on which port, with restart actions.
- **Tells you when something changes.** A card you have chosen to watch turning yellow or red, or recovering, is pushed to a command, a webhook or a system notification, once the change has been seen twice in a row.
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

Each milestone is released on its own. After M0, M2 comes next, before any further view of this machine.

### M0 Base

- A local server for macOS and Windows that only this machine can reach
- UI in English and 简体中文, switchable
- A sidebar of your local web UIs that open full-page, including services that forbid being embedded
- A clear-cache button on each embedded service that clears that service's cache and site data and nothing else: cookies and other services are left alone
- An embedded service can send browser notifications: a dialog asks once for the console, for notifications while the page is open, and once for that service's own port, for push notifications when hostdeck is closed
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

- You choose which cards to watch; only those are checked in the background, also while no console page is open
- A change counts once two checks in a row have seen it, recovery included
- Delivered first through a command you name; a webhook and the system's notifications follow
- What was last sent is remembered, so a restart does not send it again
- The console shows whether the checks are running and when each watched card was last checked

### M3 Desktop

- A desktop window showing the same pages as the browser
- A tray icon
- Start at login on macOS and Windows

### M4 Script plugins

- Starlark plugins, for cards that need more than a few lines of YAML
- Network and command access only when the plugin's manifest declares it

### Paused until review

These are built and sit outside the milestones above. They stay and receive fixes, but gain no new abilities, and until the review nothing new is added that runs, schedules or supervises programs on this machine or captures their output:

- Scheduled script tasks ([docs/TASKS.md](docs/TASKS.md))
- Restarting a service started from a shell
- Service observations: following a service's logs, and capturing a restarted service's output

The review comes two weeks after watched cards first notify through a command on main. We then ask of each whether it was used in those two weeks and what it solved. One that was not used moves to Not planned and receives fixes only.

## Run from source

hostdeck has no release yet. To try what is there, with Go 1.26 or later:

```sh
git clone https://github.com/Ike-li/hostdeck
cd hostdeck
go run .
```

Then open http://127.0.0.1:4678/.

Plugins are folders under `plugins/` in hostdeck's own folder: `~/.config/hostdeck` on macOS, `%LOCALAPPDATA%\hostdeck` on Windows. `--dir` or `HOSTDECK_DIR` picks another folder, and `--addr` another port on 127.0.0.1. The overview's **+ Add a page or plugin** card opens the add area for web pages, links and built-in plugins. The **Pages & plugins** page in the sidebar shows Enabled first, followed by disabled connections, problems and Add content, and adds the ones that come with hostdeck, and disables or enables installed ones, each change taking effect at once: a disabled plugin's folder is renamed to start with a dot, which hostdeck skips, and nothing in it changes. Hiding only removes a connection from the overview and sidebar; **Restore to workspace** brings it back. Enabling or disabling does not start or stop its external service. A plugin folder added or changed by hand is read with `hostdeck plugin reload`, when hostdeck restarts, or with the next change on that page. A web page on this machine or the local network is embedded from the same page by its address, or by dragging the address onto hostdeck; that writes a plugin folder whose `plugin.yaml` holds only `page.proxy`. Dragging a link from within the console, or dropping the console's own address, does not add a page.

The overview offers **Reset layout** only when a saved order, card width or hidden connection exists. Reset clears those preferences, and Undo restores them.

**Add content** starts with the web-page form and built-in plugins, followed by the secondary **Connection help** disclosure. It explains web page, built-in plugin and custom plugin setup. Separate one-click buttons copy web page and plugin prompts for a local agent, including this console’s actual origin, absolute plugin directory and running executable, native CLI examples for macOS shell and Windows PowerShell, and verification steps. Copying changes no data. If clipboard access fails, the prompt opens and is selected for manual copying. API and CLI additions take effect immediately; use `plugin reload` after manual file changes.

Every enabled, visible connection appears in both the sidebar and the overview. A plugin with only a monitoring card, such as Ollama, opens a generated monitor view from the sidebar. A page, link or detail plugin without monitoring gets an overview shortcut showing its destination rather than an invented service status. Plugins with invalid manifests remain visible as errors. Explicit hiding and disabling still remove connections from the workspace.

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

Paths follow [gjson](https://github.com/tidwall/gjson/blob/master/SYNTAX.md). [`examples/`](examples) has complete plugins to copy, including one whose card and detail page come from a script. `examples/jobs` lists this user's loaded launchd jobs, leaving out Apple's and ordinary applications, or on Windows the services whose program is outside the Windows directory, and can restart a running one or run one that is not.

On Windows, the jobs count includes warnings and errors. Restart is offered only for a startable running or paused service that accepts Stop. Paths are expanded and normalized before Windows system services are filtered out.

The manifest format may still change before the first release. [docs/PLUGINS.md](docs/PLUGINS.md) gives all of it: every field of `plugin.yaml`, how hostdeck runs a plugin's programs, and what they print.

## Notifications

hostdeck can run a command of yours when a card you watch changes, also while no console page is open. Put `notifications.json` in hostdeck's folder:

```json
{
  "watch": ["grafana", "jobs"],
  "command": ["sh", "notify.sh"]
}
```

```sh
# notify.sh, next to notifications.json: a macOS notification
osascript -e "display notification \"$HOSTDECK_NAME: $HOSTDECK_FROM → $HOSTDECK_TO\" with title \"hostdeck\""
```

- `watch` lists plugin IDs, the names of their folders. Only those cards are read in the background, each at its own `interval`. A plugin that is not loaded, or has no card, is skipped.
- A card is `ok`, `warn`, `error`, or `unreadable` when its reading failed. A card without thresholds is `ok` whenever it can be read.
- A change counts once two readings in a row have seen it, recovery included, so a card that flips between readings tells nothing. Nor does a card that is `ok` the first time it is seen.
- The command runs in hostdeck's folder, without a shell, for at most 30 seconds, after which it and every process it started are ended. `windows` replaces `command` on Windows, as in `plugin.yaml`. It gets hostdeck's environment and these variables:

| Variable | Value |
| --- | --- |
| `HOSTDECK_ID` | the plugin's ID |
| `HOSTDECK_NAME` | the card's name in English, or its only name |
| `HOSTDECK_NAME_ZH_CN` | the card's name in Simplified Chinese, else as above |
| `HOSTDECK_FROM` | what the command was last told of this card: `ok` for a card that was fine when first seen, empty before either |
| `HOSTDECK_TO` | the status two readings in a row have seen |
| `HOSTDECK_VALUE` | the card's value; empty when it is `unreadable` |
| `HOSTDECK_TIME` | when the reading that runs the command was made, in UTC, such as `2026-10-29T13:01:00Z` |

- What the command was told is kept in `data/notifications/sent.json`, so a restart does not tell it again. A command that cannot start or exits with an error has not told anything: it runs again at the card's next reading, and hostdeck's log says why it failed.
- `notifications.json` is read when hostdeck starts and by `hostdeck plugin reload`. A file with a mistake, such as an unknown key or a `watch` without a `command`, is refused with a line in hostdeck's log, and what was loaded before stays in use. Neither the console page nor the API writes it.
- Not yet there: the console saying whether the checks run, choosing a card to watch from the card itself, a webhook, and the system's notifications.

## Processes

Add **Processes** under Pages & plugins → Add content to see the processes visible to hostdeck, including programs without listening ports. The overview counts processes; the detail page shows names, PID, parent PID, user, CPU and resident memory in MiB, plus the executable path, parent process name, start time, uptime, thread count and accumulated CPU time. Start times explicitly use UTC; durations use hours:minutes:seconds with total hours, including values above 24. Search matches names or PIDs, and the filters separate the console from other processes. Refresh reads a new snapshot and preserves the search and filter. The card caches its count for one minute; detail reads are on demand.

This plugin is read-only. It never ends processes, requests administrator privileges, reads command arguments or environments, or saves process history. A denied optional read is labelled unavailable without losing the process; a failed process-table read reports an error instead of zero. macOS uses the CPU percentage reported by `ps` and RSS, and one optional `top` snapshot for thread counts (at most two seconds and 1 MiB of output); Windows samples process CPU time over at least 250 ms and uses the working set. 100% CPU represents one fully used core and can be exceeded. These platform readings are estimates and need not match Activity Monitor or Task Manager. Windows shows owner SIDs without a network account lookup, and excludes the PID 0 idle-time pseudo process. A refresh is a live snapshot, so a process may exit while its fields are read.

## Disk history

Add **Disk history** under Pages & plugins → Add content. This is separate from **Disk space**, which shows free capacity. It runs inside hostdeck on macOS and Windows; no Python, database server, extra port or disk-history installation is needed. Installing or opening it does not scan anything.

Open its detail page and confirm **Scan now**, or **Start hourly tracking** to scan now and then roughly hourly while hostdeck runs. **Pause hourly tracking** retains history; **Cancel scan** also pauses tracking and retains the previous snapshot. Closing the page does not stop a scan. While this page is open, a running scan is checked every two seconds and its saved results appear automatically; idle pages check once a minute. Refresh also reads the latest results. After a single reading the card says how to see changes. A reading more than two hours old is a warning only while hourly tracking is on. Stopping hostdeck interrupts the current scan; enabled tracking resumes on the next start. Disabling or trashing the plugin cancels its scan and timer; enabling it resumes saved tracking. Hiding it only changes the workspace layout.

The default target is the current user's home folder. To select other targets, create `plugins/disk-history/config.json` inside your hostdeck data directory, with 1–8 absolute directory paths. JSON backslashes must be escaped on Windows:

```json
{"roots":["/absolute/path/to/folder"]}
```

```json
{"roots":["C:\\Users\\Example\\Documents","D:\\Projects"]}
```

Changes to this file apply to subsequent scans; an already running scan finishes its original target. Paths are local configuration, never accepted from a web request. Each configured target has its own history. The overview summarizes the first target and its three largest top-level changes, in whole hours (minutes when the readings are less than an hour apart); for the home folder it reads like "Past 24 hours · 122.5 GB in the home folder" with rows such as `~/code`; the dedicated detail page selects a configured target and a 1-hour, 24-hour (default), 7-day or 30-day range. A centred reading surface shows current size and change together, then a size curve, period changes and direct subfolders ordered by absolute change. Select a bar to compare that period; open a folder and use the breadcrumb to return. The list initially shows 20 folders and can reveal the rest. The range ends at the latest saved scan; its baseline is the newest scan at or before the range start, or the earliest available scan. Actual comparison times are always shown, in the browser’s local time zone. The curve retains actual sample times and breaks where readings cannot compare; missing observations are not fabricated as deletions. **Scan readings and measurement notes** holds the selected range’s recorded values. Three-level and capped-summary boundaries are explained in place. Opening a folder reads only saved summaries; it does not scan, add targets or import old homedeck history.

Measurements are the disk space regular files occupy, using decimal units. A sparse file counts only the blocks it holds, so a disk image such as Docker's counts what it stores rather than its length. On macOS a file with several hard-link names counts once. On Windows, sparse and compressed files count the bytes they store, other files count their length, and each hard-link name counts separately. APFS clones count in full for every copy. Readings saved before disk space was measured counted file lengths; they are not compared with newer ones and age out with the rest of the history. Directory summaries go three levels deep, but their totals include deeper files. Links, Windows reparse points, other mounted volumes and the plugin's own files are excluded. A scan is a live traversal rather than an atomic filesystem snapshot. Unreadable entries, such as folders macOS privacy protection keeps from hostdeck, are left out of the reading, and the card says how many. Two readings that missed the same entries compare what they did read; when they missed different entries, the change is withheld and the card says why. An entry that disappears during a scan is gone, not unreadable. Failed or cancelled scans do not replace saved snapshots. A scan has a 20-minute deadline.

History lives in this plugin's `data/history.json`, independently of homedeck's old SQLite data. It retains up to 30 days and 744 snapshots per target, at most 256 directory summaries per snapshot, and at most 64 MiB in total. The root total remains when the directory-summary limit is reached; shallower summaries take priority and the detail page reports that limit. Corrupt history is reported and never silently replaced. A process-held OS lock refuses a second writer; process exit releases it. Back up the plugin folder to keep its history; purging it from the trash deletes that history too.

The compiled plugin uses `exec.command: ['@hostdeck', disk-history, card]` (or `detail` / `action`). `@hostdeck` is a reserved in-process dispatcher for these operations, not a PATH executable or a shell. Other plugins keep their existing command protocol.

## CLI for agents

Keep the console running and use the same version of hostdeck for its CLI. Commands never start another console. `--url` selects its origin and defaults to `http://127.0.0.1:4678`; only HTTP on `127.0.0.1` is accepted. Credentials, other hosts, URL paths and redirects are refused. The UI’s copyable prompts use the running executable’s absolute path, including when the console was started with `go run`.

From this checkout, replace `hostdeck` below with `go run .`. For a built executable outside PATH, use its path (`./hostdeck` on macOS or `& .\hostdeck.exe` in PowerShell).

```sh
hostdeck help
hostdeck page add --url http://127.0.0.1:4678 --address http://127.0.0.1:3000/ --name Grafana --mode proxy
hostdeck page add --url http://127.0.0.1:4678 --address https://example.com/ --name Notes --mode link
hostdeck page update --url http://127.0.0.1:4678 --id PAGE_ID --name "New name" --icon N
hostdeck plugin list --url http://127.0.0.1:4678
hostdeck plugin catalog --url http://127.0.0.1:4678
hostdeck plugin install --url http://127.0.0.1:4678 --id ollama
hostdeck plugin validate --from /path/to/my-plugin
hostdeck plugin install --url http://127.0.0.1:4678 --id my-plugin --from /path/to/my-plugin
hostdeck plugin disable --url http://127.0.0.1:4678 --id my-plugin
hostdeck plugin enable --url http://127.0.0.1:4678 --id my-plugin
hostdeck plugin reload --url http://127.0.0.1:4678
```

Use named flags after the command. `page update` accepts only supplied `--name`, `--icon`, `--proxy` or `--link`, and `--card-url` for an existing HTTP card. Proxy and link cannot be combined. It preserves comments, commands and unrelated values. `plugin list` and `plugin catalog` both return the full catalog: actual paths, installed entries and available built-ins.

`plugin validate` is offline: it uses the same `schema: 1` parser and the CLI process environment without executing plugin programs or contacting services. A custom install needs a local source folder with `plugin.yaml`; it queries the selected console for its actual plugin directory, validates and copies the folder under a hidden staging name, publishes it, then reloads. It refuses existing enabled or disabled IDs, symlinks, special files and sources containing the destination. Nothing is downloaded. A per-ID lock serializes CLI installations; an interrupted process can leave a lock or hidden staging folder for manual cleanup after that process has ended. Backups and source folders belong outside the active plugin directory.

All connection commands, including `help`, write one JSON result to stdout. Success has `ok: true` and `data`; failure has `ok: false` and `error.code` / `error.message`, sometimes with recovery data. Exit **0** means success, **1** an operation/validation failure, and **2** invalid command arguments. In PowerShell, inspect `$LASTEXITCODE` after each native call. If files were saved but reloading failed, `data.saved` is true. A local custom install also returns `data.path`; API changes include the ID when it is known: fix the error and reload rather than install a duplicate. `plugin reload` returns nonzero for broken plugins or forwarding failures while retaining the loaded state on a fatal reload error.

What `data` holds:

- `plugin validate`: `data.valid`, `data.schema` (the manifest version this hostdeck reads), `data.path` (the folder's absolute path) and `data.name`.
- `page add`, `page update`, `plugin install`, `plugin enable` and `plugin disable`: `data.id`, `data.path` (the plugin's folder, starting with a dot when it is disabled), `data.saved`, `data.loaded`, `data.disabled`, `data.available` (enabled and loaded without an error) and `data.plugin`, the console's own entry for the plugin.
- `plugin list`, `plugin catalog` and `plugin reload`: the console's catalog. Its `pluginDir` and `installed` list, each entry with `id`, `loaded`, `disabled`, `broken` and `error`, are what the CLI itself reads.

`error.code` is one of the CLI's own codes, or a code the console answered with, which the console words under `error.<code>` in [web/i18n/en.json](web/i18n/en.json):

| Code | Meaning |
| --- | --- |
| `usage` | The command, a flag or `--url` is not valid; exit code 2 |
| `invalid_manifest` | The `plugin.yaml` in `--from` does not load; the message says why |
| `invalid_source` | The source folder cannot be read, holds a link or special file, or contains the plugin directory |
| `already_added` | An enabled or disabled plugin already has that ID |
| `install_busy` | Another install of that ID is running |
| `install_failed` | Copying or publishing the folder failed |
| `connection_failed` | No console answered at `--url` |
| `request_failed` | The request could not be made, or the console refused it without a code |
| `invalid_response` | The console's answer was not what this CLI reads, as from a console of another version |
| `not_loaded` | The files were saved, but the plugin did not load; see `data.plugin` |
| `plugins_unavailable` | `plugin reload` finished, but an enabled plugin is broken or did not load; see `data.installed` |

From the first release, the commands, flags, exit codes, and the keys and codes named here are kept; new ones may be added. Read `ok` and the exit code first, ignore keys you do not know, and treat a code you do not know as a failure. `error.message` is for people, and its words may change. `data.plugin` and the rest of the catalog follow the console's own API, which may change in any release. See [Contracts and compatibility](docs/ARCHITECTURE.md#contracts-and-compatibility).

`loaded` means hostdeck loaded the manifest, not that the external service is healthy. Open its card/page to verify actual readings. CLI validation and server loading use their respective environments; a new secret must be supplied to the running server, which may need an authorized restart. Manual edits can be activated with `plugin reload` without a restart. Validation and copying themselves do not run scripts; later card/detail requests run trusted plugin programs as before. The web UI still cannot submit or write commands.

## How it works

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) describes the pieces, what happens on each request, and the security model. [docs/PLUGINS.md](docs/PLUGINS.md) is the plugin contract. [docs/DESIGN.md](docs/DESIGN.md) is the console's design system: its tokens, components and patterns, with their values, their reasons and the tests that keep them.

## Platforms

macOS and Windows. Windows is **experimental** until it has been tried by hand; for now only CI covers it.

## License

[MIT](LICENSE)

Background jobs can be searched by part of their name or ID and filtered by running, not running or needs attention, with a disabled group on Windows. Counts follow the search. Filters last through refreshes and language changes in the current page session.

Job details use expanded compact fields with the runtime state and action beside the name. macOS adds PID, configured command, launch rules, configuration and both log paths. Windows adds service identity, PID, start mode, account and command. The list has no log text. Opening a job shows a scrolling tail of at most 256 KiB, or up to 100 matching service events on Windows. Run and Restart say what the job is doing afterwards. Unreadable optional information is explained on the task. Docker monitoring is a separate M1 plugin.

The built-in **Docker containers** plugin provides a compact searchable container list, counted state filters and overview counts. It shows selected metadata and bounded recent stdout/stderr logs using the installed Docker CLI. The engine must already be running on this computer; remote endpoints are refused, and installation does not start Docker or a container. Local Unix sockets, named pipes and literal loopback TCP endpoints are supported. Confirmed actions start stopped or newly created containers and restart running ones. Paused and transition states offer no action; all actions recheck the full container ID and current connection.

## Services started by hand

Add **Services started by hand** under Pages & plugins → Add content. It lists TCP listeners started from a terminal or a session, such as a program left running in Terminal, iTerm, Windows Terminal, VS Code or a Claude session. The page shows the working directory and the command. A confirmed action stops that process, or restarts it from this console; the console itself cannot be stopped here. A restarted process keeps running after the original terminal closes. After hostdeck exits it keeps running and leaves this list. launchd jobs, Windows services and ports an app opens for itself are not listed. The card names each process and the ports it holds; the page groups them by where they were started. For every TCP listener on this machine, see **Listening ports**.

## Listening ports

Add **Listening ports** under Pages & plugins → Add content. It lists every TCP port that is listening on this machine, with the bind address (127.0.0.1 vs 0.0.0.0), PID, process name, working directory and command line (truncated past 512 characters). The detail page is searchable and grouped by bind address. hostdeck itself, its ancestors, and the ports it forwards are marked and still listed. Confirmed Stop ends a listening process and its children; hostdeck itself, its ancestors, forwarded ports and PID 1 are protected and cannot be stopped here. UDP remains later M1 work.

## Scheduled script tasks

The built-in `scheduled-tasks` plugin installs an empty `tasks.json`. The detail page can create, edit and delete a task, which rewrites `tasks.json` and refreshes scheduling immediately, and can save a script file under that plugin's `scripts/` directory. Saving a script does not add a task or run it. A script row can run one allowed command without writing `tasks.json`, or open the task form with the default command filled in. A hand edit of `tasks.json` followed by `plugin reload` still loads. Confirmed manual Run and Stop, timeouts, results, the last ten run summaries and bounded logs are available; Cron and fixed-rate scheduling run while hostdeck is running. Pause/Resume controls future starts; Stop selects one run. Schedules never catch up downtime or automatically retry. Configuration is loaded at startup or explicit `plugin reload`. A refused configuration retains the last valid definition and current runs while reporting the problem. Reads start no script; script output need not be JSON. Nonzero exits and timeouts are distinct results. Execution writes only the plugin's own `data/`, and known referenced secrets are redacted from logs. See [TASKS.md](docs/TASKS.md) for fields and rules.


Scheduled-task recovery and storage boundaries are implemented: unfinished runs are interrupted and paused; Resume probes cleanup without killing old PIDs, and unresolved tasks do not restart. The owner lock excludes a second writer; corrupt state refuses replacement. Retention commits reference changes before deleting logs, preserving prior logs on a refused write. Undurable results show interruption and storage failure. Scheduling persists anchors, cursors and missed counts; it permits one run per task and four runs per installed plugin.

Installation also includes a disabled `tasks.example.json`; the empty detail explains its absolute path and that the page can create a task or a script, while a hand edit and reload still work. The connection-help plugin prompt includes the complete task schema and commands for this console.

Built-in Background jobs now offers **Logs and status** with continuous bounded readings and a separate **Recent operations** area. Operation completion and service health are shown separately. Existing logs are read in place; Windows shows service management events, not application logs. No job/service configuration is changed. Upgrade the executable to use the observation interface. Existing installed plugins are never overwritten: locally changed or older jobs helpers continue to use the compatible one-shot log view until their files are explicitly updated by the owner.

Listening ports and Services started by hand now share this observation view. A confirmed PID plus start time can associate an existing job source; on macOS only actual stdout/stderr regular files are inspected. Terminal output is left in its terminal. Windows explains when external application logs cannot be identified reliably. No installed manifest needs editing for the native plugins.

Confirmed manual-service restarts now capture subsequent stdout/stderr through an independent process of the same executable. Preparation and a 128 MiB directory budget check finish before the original service is stopped. A failed preparation leaves it running; a failed new startup is reported without retry. Services and collection continue after normal or abrupt console exit, and their verified PID, exit result and bounded output remain accessible through Recent operations even after they leave the current list. Each run rotates two 2 MiB files; completed captures retain up to three runs per service for seven days. External logs are not archived in full. All new data is under the selected `--dir`/`HOSTDECK_DIR` directory's `data/service-observations/`. Update the executable and refresh the console; no installed plugin file is changed automatically. Deployment and real-service verification are separate owner actions.
