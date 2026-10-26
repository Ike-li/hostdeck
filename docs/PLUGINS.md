# Plugins

[简体中文](PLUGINS.zh-CN.md)

This is the plugin contract: what a plugin folder holds, every field of `plugin.yaml`, how hostdeck runs a plugin's programs and what they print. It is `schema: 1`, which may still change before the first release; [Compatibility](#compatibility) says what is kept after it. `internal/plugin` reads the manifest; tests check that every field it accepts is named here, in both languages.

The [README](../README.md#run-from-source) has three short manifests to start from, and [`examples/`](../examples) holds complete plugins, among them disk space and background jobs, whose cards and detail pages come from scripts for macOS and Windows.

## The folder

A plugin is a folder under `plugins/` in hostdeck's folder, holding a `plugin.yaml`. The folder's name is the plugin's ID: it is the sidebar address, the key in `layout.json` and `ports.json`, and what the CLI's `--id` names. The CLI takes an ID that is not empty, does not start with a dot, holds no `/`, `\`, `:`, line break or NUL, and has no space at either end.

- A folder whose name starts with a dot is skipped. `.disk` is `disk` disabled, and `.trash` holds the plugins moved to the trash.
- A plugin's programs run in its folder, so a script beside `plugin.yaml` is named by its file name.
- A plugin may keep files of its own in its folder, as disk history keeps `config.json` and `data/`. Purging the plugin from the trash deletes them with it.
- hostdeck reads the plugins when it starts, after each change on the plugins page, and on `hostdeck plugin reload`. `hostdeck plugin validate --from <folder>` checks a folder by the same rules without running anything.

## plugin.yaml

One YAML document. A field hostdeck does not know is an error that names its line, so a typo shows up rather than being ignored. A plugin whose manifest has a mistake is listed as an error that says what is wrong; the other plugins still load.

| Field | Type | Needed | What it is |
| --- | --- | --- | --- |
| `schema` | integer | yes | `1` |
| `name` | text | yes | The plugin's name in the sidebar, on its card and on the plugins page |
| `description` | text | | What the plugin does and what to change before using it; the plugins page shows it |
| `icon` | string | | Drawn as written, in a small square beside the name: keep it to one character or one emoji. Without it, the name's first character |
| `page.proxy` | URL | | A web UI on this machine or the local network, embedded in the console through a forwarding port of its own |
| `page.link` | URL | | A web page that the sidebar entry opens in a new tab |
| `card.interval` | duration | `1m` when left out | How long a successful reading is kept |
| `card.http.url` | URL | for an HTTP card | What the card asks with GET; may hold `${env:NAME}` |
| `card.http.headers` | map of strings | | Request headers; their values may hold `${env:NAME}` |
| `card.value` | gjson path | for an HTTP card | Where the card's value is in the response |
| `card.label` | text | | Shown with the value |
| `card.warn` | threshold | | When the value turns yellow |
| `card.error` | threshold | | When the value turns red |
| `card.rows[].name` | text | | A row's name |
| `card.rows[].value` | gjson path | in each row | Where the row's value is in the response |
| `card.rows[].warn` | threshold | | When the row turns yellow |
| `card.rows[].error` | threshold | | When the row turns red |
| `card.exec.command` | program | for a program card | Prints the whole card |
| `card.exec.windows` | program | | Runs instead of `command` on Windows |
| `detail.exec.command` | program | with `detail` | Prints the items of the plugin's detail page |
| `detail.exec.windows` | program | | Runs instead of `command` on Windows |
| `actions.exec.command` | program | with `actions` | Runs an action on one item of the detail page |
| `actions.exec.windows` | program | | Runs instead of `command` on Windows |

How the parts go together:

- A plugin has a `name` and at least one of `page`, `card` and `detail`.
- `page` has exactly one of `proxy` and `link`.
- `page` and `detail` are each a sidebar entry, and a plugin has one: not both. Either may have a `card`.
- `card` has exactly one of `http` and `exec`. `value`, `label`, `warn`, `error` and `rows` go with `http`; a program card prints its own.
- `actions` needs `detail`, since an action acts on one of its items.

### Values

- **text**: one string for every language, or one per language, as in `{en: Status, zh-CN: 状态}`. The languages are `en` and `zh-CN`; any other key is an error. The page shows the console's language, else English, else the first one given.
- **URL**: `http` or `https`, with a host. A `page.proxy` host must resolve only to loopback, private or link-local addresses: the proxy checks that each time it connects, because the service is sent the browser's Host, `127.0.0.1:<forwarding port>`, which a site on the internet would not answer to.
- **duration**: as Go writes durations, such as `30s`, `5m` or `1h30m`, and more than zero.
- **gjson path**: a path into the JSON response in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md). The value is the text gjson gives for what the path finds, so a number keeps its digits and an object is its JSON.
- **threshold**: exactly one of these keys:

  | Key | The value meets it when it is |
  | --- | --- |
  | `above` | a number greater than the one given |
  | `below` | a number less than the one given |
  | `equals` | written the same as the value given |
  | `not` | written other than the value given |

  `error` is checked before `warn`, so when both are met the value is red. With thresholds and neither met, it is green; with no thresholds, it has no colour. `above` or `below` on a value that is not a number makes the card an error rather than a colour.
- **program**: a list of the program and its arguments, run without a shell: `[sh, card.sh]`, or `[powershell, -NoProfile, -NonInteractive, -ExecutionPolicy, Bypass, -File, card.ps1]` on Windows. A program named without a folder is looked up on `PATH`. A list that starts with `'@hostdeck'` runs a program built into hostdeck, such as `['@hostdeck', disk-history, card]`; a program of that name is never looked up on `PATH`.

### Secrets: `${env:NAME}`

`${env:NAME}` may be written in `card.http.url` and in the values of `card.http.headers`, and nowhere else. NAME is letters, digits and underscores, not starting with a digit; any other `${…}` there is an error. A variable that hostdeck was started without makes the plugin an error that names it.

The value is sent to the service and written nowhere. In a card's error it shows as `***`, also where the error quotes it or escapes it for a URL; a value shorter than three characters is left as it is, since hiding it would hide those characters everywhere in the message. A program's arguments are not filled in: a program reads the variables from its own environment, which is hostdeck's.

## HTTP cards

- `card.http` asks `url` with GET and `headers`. It ignores the environment's proxy settings and does not follow redirects: a redirect is an error that names its status, as is any status other than 2xx.
- The response must be JSON, at most 4 MiB. A `value` path that finds nothing is an error that names the path, and so is a row's.
- A reading may take up to 30 seconds. The overview waits 5 seconds, then shows the card as still being read, with its last value if it has one, and asks again. A successful reading is kept for `interval`; a failed one is not kept, so a service that recovers shows at once.

## Programs

### How a program runs

- `command`, or on Windows `windows` when it is given, runs in the plugin's folder with hostdeck's environment. A card program and a detail program are given no arguments; an action program is given two more, see [Action programs](#action-programs).
- Each run has 30 seconds. At the deadline the program and every process it started are ended: a process group on macOS, a Job Object on Windows. Processes it leaves running when it exits are ended too, and hostdeck waits at most one second for output after the program exits.
- stdout holds exactly one JSON value, which may start with a UTF-8 byte order mark; nothing but white space may follow it. A field this page does not name is an error, so a typo shows up. hostdeck keeps the first 4 MiB of stdout and the first 64 KiB of stderr.
- Exit code 0 is success. Any other exit code is an error that shows the code and the last line of stderr, and stdout is not read. So write diagnostics to stderr, and report a failure meant for people as `{"error": …}` with exit code 0.
- Windows PowerShell 5.1 reads a script without a byte order mark in the system's code page, so save scripts as UTF-8 with a BOM, and have them write UTF-8: the examples set `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false`. Its first start can take many seconds, which is why a card may come back still being read.

### Card programs

A card program prints the whole card:

```json
{"status": "warn", "value": 42, "label": "waiting", "rows": [{"name": "Failed today", "value": 3, "status": "error"}]}
```

| Field | Type | What it is |
| --- | --- | --- |
| `status` | `ok`, `warn`, `error`, or left out | The card's colour; left out, it has none |
| `value` | string or number | The card's value |
| `label` | text | Shown with the value |
| `rows[].name` | text | A row's name |
| `rows[].value` | string or number | A row's value |
| `rows[].status` | `ok`, `warn`, `error`, or left out | A row's colour |
| `error` | text | When given, the card shows these words as the plugin's own error, and nothing else |

Any other status is an error.

### Detail programs

A detail program prints the items of the plugin's sidebar page, or `{"error": …}`, which the page shows instead of them:

```json
{"items": [{"id": "backup", "name": "Nightly backup", "status": "ok", "value": "ran at 02:00", "fields": [{"name": "Folder", "value": "/Volumes/Backup"}], "log": "02:00 started\n02:14 done", "actions": [{"id": "run", "label": {"en": "Run now", "zh-CN": "立即运行"}}]}]}
```

| Field | Type | What it is |
| --- | --- | --- |
| `presentation` | `compact` or omitted | Optional compact list layout; omission keeps the existing layout |
| `groups[].id` | string | Optional unique, nonblank group ID; `all` is reserved |
| `groups[].label` | text | Nonblank group label, in display order |
| `items[].group` | string | A declared group ID, required on every item when groups are given; groups enable session-only name/ID search and counted filters |
| `items[].id` | string | Required, and different for each item; the action program is given it |
| `items[].name` | text | Required. The item's heading. Missing, `null`, empty or whitespace-only text, or a language object with no nonblank text is an error, and the page shows no items. One language with nonblank text is enough |
| `items[].status` | `ok`, `warn`, `error`, or left out | The item's state, shown in words |
| `items[].value` | string, number or localized text | The item's main value |
| `items[].fields[].name` | text | A field's name |
| `items[].fields[].value` | string, number or localized text | A field's value |
| `items[].log` | string | Shown under Last lines of the log, in a box of its own that scrolls; print the last lines, not a whole file |
| `items[].actions[].id` | string | Required; the action program is given it |
| `items[].actions[].label` | text | The action's button |

### Action programs

An action's button needs a second click. hostdeck then runs the detail program again, and runs the action only if that item still offers it; otherwise the page says so and reads the list again. One action runs at a time, across every plugin. The action program runs as `<command…> <action id> <item id>`, and runs to its end or its deadline even if the page that asked for it is closed, so that a restart is never stopped halfway. The two IDs are ones your detail program printed a moment before; treat them as data, never as code.

The program may print nothing, `{"message": …}`, which the page shows as the result, or `{"error": …}`; both are text. With nothing printed, the page says the action is done. The page reads the detail again a moment later.

## Compatibility

Until the first release, `schema: 1` may still change, and the commit that changes it says what an existing plugin needs. From the first release, the manifest and what programs print only gain optional fields and values within `schema: 1`; nothing is removed, renamed or given another meaning. A plugin that uses a newer field needs a hostdeck that knows it: an older one refuses the plugin and names the field's line. A change that cannot be made this way takes `schema: 2`, and its release says how to move a plugin. [ARCHITECTURE.md](ARCHITECTURE.md#contracts-and-compatibility) gives the rules for every contract.

## Trust

Plugins are trusted: they are folders you put there, and their programs run with your rights. The console's web page cannot write a program into a manifest; the security model in [ARCHITECTURE.md](ARCHITECTURE.md#security-model) says what each change from the page can write. Copy a plugin only from a source you trust.

Background jobs use compact grouped details. macOS reads configuration with system plutil and shows PID, configured arguments, launch rules, configuration and log paths. Log tails read at most 16 KiB and 20 lines per distinct file within one item. Windows shows service identity, PID, start mode, account and full command, and matches structured properties in the latest 200 Service Control Manager events, up to 20 per service. All previews share a 512 KiB page budget. Missing configuration, inaccessible logs, no matching events and truncation are local field messages; they do not change job health or action permissions. Existing string/number detail values keep their behavior; localized values use the same en/zh-CN text objects as labels and require a newer hostdeck.

## Native Docker monitoring

The built-in `docker` plugin uses `['@hostdeck', docker, card]` and `['@hostdeck', docker, detail]` with the existing card/detail JSON contract. It needs a Docker CLI on hostdeck's PATH and a running engine. Installation does not start Docker or any container, and creates only the manifest.

The effective Docker context is selected with Docker's environment precedence (`DOCKER_CONTEXT` before `DOCKER_HOST`, otherwise the selected context). The plugin accepts a local Unix socket, `npipe:////./pipe/...`, or a literal loopback TCP address. It pins that endpoint for every daemon command and removes context/host overrides from that child's environment. TLS settings for loopback TCP can be supplied through Docker's existing environment variables; context-specific TLS material is not imported. Remote, named-host TCP and SSH endpoints are refused.

Details show image, state, health, times, exit status, restart policy/count, ports, networks, mounts and command arguments; environment variables and health-check output are not requested. The overview running count includes unhealthy running containers; its cleanly stopped/not-started row excludes failures. Running containers with an unhealthy check and containers that exited unsuccessfully need attention; a clean stop or a newly created container does not. Paused containers have their own group. A starting health check is shown without declaring a failure.

Recent logs combine stdout and stderr, request the last 20 lines without following, retain at most 16 KiB per container and share 512 KiB per page. The whole page has five seconds for optional log reads, and each has at most one second. Unsupported logging drivers, missing logs or time/size limits produce local notes without losing container information. A failed list or inspection refuses the whole reading instead of reporting a healthy zero.

Docker actions use `['@hostdeck', docker, action] <action-id> <item-id>`. `start` is offered only for created/exited containers, `restart` only for running ones, including unhealthy running containers. Paused, restarting, removing, dead and unknown states offer neither. As for other detail actions, a second click and a successful re-read are required. The native program additionally validates the opaque endpoint-bound item ID and re-inspects that full 64-character container ID before invoking exactly one start/restart command. It never falls back to a name or a shortened ID.

The command honors Docker's configured stop timeout. The console's 30-second deadline still ends the CLI process tree, but an operation already accepted by the Docker engine may continue. A CLI timeout therefore reports an uncertain result and tells the user to refresh, rather than claiming success or cancellation. These operations add no manifest keys or saved files and require a newer binary that supports the Docker action program.

## Scheduled task configuration preview

The first scheduled-task slice provides configuration preview. `scheduled-tasks` installs `plugin.yaml` and an empty `tasks.json`; `@hostdeck tasks card|detail` validates and displays tasks in process. Reads execute no command and write no state; actions and automatic scheduling are not available yet. [TASKS.md](TASKS.md) defines fields, defaults and the planned complete behavior. Existing plugins and files need no migration.
