# Scheduled script tasks

[简体中文](TASKS.zh-CN.md)

**Proposed v1 contract; not implemented.** This document defines the first scheduled-script plugin. Creating these files in today's hostdeck does not load tasks or run scripts. The implementation must add its parser, examples, tests and agent instructions together before claiming support. No new field in `plugin.yaml`, CLI command or HTTP endpoint is introduced by this document.

One installed plugin owns multiple tasks. The existing [plugin contract](PLUGINS.md) supplies its card, detail and confirmed actions. A task is an ordinary local program; it need not produce plugin JSON. Scheduling runs inside hostdeck, with no additional service or port: closing the browser does not stop it, but exiting hostdeck does. This version does not install launchd or Windows Task Scheduler entries, download scripts, install dependencies, edit commands from the browser or automatically retry failed runs.

## Files and versions

The plugin folder contains locally edited `tasks.json` and a plugin-owned `data/` directory. `tasks.json` carries `version: 1`. `data/state.json` carries `version: 1`, task runtime preferences, interval anchors, scheduling cursors and run records; logs live under `data/logs/`. These files belong to this plugin, not to every hostdeck plugin. Task version 1 and manifest `schema: 1` are independent.

A configuration is one UTF-8 JSON object, at most 1 MiB, with exactly one value and no duplicate object keys. Unknown fields, unsupported versions, wrong types, explicit nulls, duplicate task IDs or any invalid task refuse the entire candidate configuration. Required nested `schedule` and `exec` values must be objects, and a schedule must select exactly one of `cron` and `every`. Startup with invalid configuration runs nothing and reports why; a refused reload keeps the last valid configuration. Loading, validating or reading a card/detail never launches a script. After a successful explicit reload, automatic scheduling uses only future occurrences. Task changes are not watched continuously.

State writes are atomic, under one OS-held owner lock per installed plugin. A second process may read snapshots but cannot schedule, run, change preferences or repair state while another owns it. Corrupt or unsupported state refuses all execution and writes until repaired locally; it is never silently replaced. Future implementations must read earlier supported state versions. Configuration readers reject fields they do not understand; state readers may ignore added optional fields within a supported version. Incompatible configuration changes require a new version and documented migration.

## Task definition

The root fields are `version` (required integer `1`) and `tasks` (required array, zero to 64 tasks). All field names below belong to `tasks.json`; none is added to `plugin.yaml`.

| Field | Type / default | Meaning |
| --- | --- | --- |
| `tasks[].id` | required string | Stable identity, 1–64 ASCII letters, digits, underscores or hyphens; start with a letter or digit. Unique within this installed plugin; do not reuse a deleted task's ID for a different purpose. |
| `tasks[].name` | required string | Human-readable name, 1–200 Unicode code points after trimming outer whitespace. |
| `tasks[].enabled` | required boolean | Whether automatic triggers and manual Run are allowed. Installing the plugin does not override this value. |
| `tasks[].schedule.cron` | string | Five numeric fields: minute, hour, day of month, month, day of week. Mutually exclusive with `every`. |
| `tasks[].schedule.timezone` | string | Required with `cron`: `UTC` or an IANA time-zone name such as `Asia/Shanghai`. Not allowed with `every`; no implicit machine-local zone. |
| `tasks[].schedule.every` | duration string | Fixed-rate interval from 1 minute to 365 days inclusive, such as `30m` or `24h`. Mutually exclusive with `cron`. |
| `tasks[].exec.command` | required string array | Executable followed by separate arguments, with at least one element and a nonblank executable. No NUL in an element. |
| `tasks[].exec.windows` | optional string array | Same rules; replaces `command` on Windows. Omission uses `command` on both systems. |
| `tasks[].exec.dir` | optional string, `.` | Absolute directory or a directory relative to the installed plugin folder. Resolved before execution; it must exist. |
| `tasks[].exec.env` | optional string map, `{}` | Environment overrides, each value exactly `${env:NAME}`, referring to hostdeck's environment. No literal secret or argument interpolation. |
| `tasks[].timeout` | optional duration string, `30m` | Execution deadline, from 1 second to 24 hours inclusive, including startup. Cleanup has at most one additional second. |

Durations use Go's positive duration syntax (`1h30m`, not calendar months or a `d` suffix). Neither blank nor null means a default. Configuration bounds are enforced before publishing it; directory/executable availability and environment references are checked again at execution, where failures become failed runs without starting the script.

Environment keys and referenced names match `[A-Za-z_][A-Za-z0-9_]*`. Keys differing only by ASCII case are refused, including on macOS, to keep Windows overrides unambiguous. The child otherwise inherits hostdeck's environment. Overrides replace an inherited key using the target system's case rules. A missing reference fails that run; environment values are not copied into saved run records. This reference syntax is specific to task `exec.env` and does not expand the locations allowed in the existing manifest.

### Configuration example

This illustrates the proposed format, not an executable feature in the current release. The scripts and interpreters must already exist locally. Both tasks start disabled, so enabling scheduling is explicit.

```json
{
  "version": 1,
  "tasks": [
    {
      "id": "daily-backup",
      "name": "Daily backup",
      "enabled": false,
      "schedule": {"cron": "0 2 * * *", "timezone": "Asia/Shanghai"},
      "exec": {
        "command": ["python3", "scripts/backup.py"],
        "windows": ["py", "-3", "scripts/backup.py"],
        "dir": ".",
        "env": {"BACKUP_TOKEN": "${env:BACKUP_TOKEN}"}
      },
      "timeout": "2h"
    },
    {
      "id": "collect-local-data",
      "name": "Collect local data",
      "enabled": false,
      "schedule": {"every": "30m"},
      "exec": {"command": ["node", "scripts/collect.mjs"]},
      "timeout": "5m"
    }
  ]
}
```

## Scheduling semantics

- Cron accepts numeric `*`, lists, inclusive ranges and positive steps. Ranges must increase and stay within each field's bounds: minutes 0–59, hours 0–23, month days 1–31, months 1–12 and weekdays 0–7 (Sunday is 0 or 7). Steps apply to `*` or a range, such as `*/15` or `9-17/2`; names, macros, six-field expressions, `?`, `L`, `W` and `#` are refused. When both day fields are restricted, either may match; otherwise the restricted one must match. Only literal `*` counts as unrestricted. A schedule with no matching date in a full 400-year Gregorian cycle is refused.
- A nonexistent local time during a daylight-saving jump is skipped. A repeated local minute runs once, at its first occurrence. Historical occurrences are never replayed. The timezone is used only to find due instants; records use UTC timestamps.
- An interval's first occurrence is one interval after it first becomes enabled and unpaused. That anchor is persisted; later occurrences are `anchor + n × every`, independent of completion time. Restart or ordinary pause/resume preserves it. Changing schedule resets the anchor when the changed schedule first becomes enabled and unpaused. Manual runs do not move it.
- Each scheduled occurrence is identified by task ID, configuration revision and its UTC due instant. A persisted cursor prevents reconsidering the same or earlier instants, including after the clock moves backwards. A forward clock jump or delayed scheduler may dispatch only the latest due occurrence, and only if it is less than 60 seconds late; older occurrences are missed. Record missed counts and their time range in bounded task state rather than creating one record per missed instant.
- Startup, successful configuration reload and resume advance the cursor past all occurrences through the current time. They never catch up missed work. Resuming does not immediately run a script. A first interval anchor is created at this point, before advancing the cursor.
- A task has at most one active run, including startup and cleanup. A due occurrence during that run becomes `skipped` with reason `overlap`. Runtime pause defaults to false for a newly configured task. At most four tasks run simultaneously per installed plugin; a due occurrence with no slot becomes `skipped` with reason `capacity`. There is no waiting queue. Manual Run during overlap or exhaustion is refused without creating a run; otherwise it uses the same runner as a scheduled trigger.
- Automatic triggers and manual Run require both an enabled configuration entry and an unpaused runtime preference. Pause prevents future starts and lets an active run finish. Stop cancels a selected active run without pausing future triggers. Neither retries it. Failures and timeouts never trigger an automatic retry.

These rules provide a stable schedule, not exactly-once script side effects. A crash can occur between saving a start marker and actually starting a process, or after a script changes a file but before its result is saved. Scripts that need safe repetition must implement their own idempotency.

## Execution and lifecycle

The runner takes an immutable snapshot of a task and a trigger (`scheduled` with a UTC due instant, or `manual`). It reserves the task/slot and persists a `starting` record before spawning. If that write fails it launches nothing. The scheduler, manual controls, storage and process runner communicate through internal Go interfaces; those interfaces and console HTTP shapes are not external compatibility contracts.

`exec.command` and `exec.windows` run without an implicit shell. Pipes, redirects, wildcard and variable expansion are available only by explicitly invoking an appropriate shell. Relative directories start at the installed plugin folder; a bare executable uses the child's PATH and relative executable paths start at the resolved working directory. No shell profile or interpreter installation is implicit. `@hostdeck` is reserved and is refused as a task executable; task scripts cannot recursively invoke native plugin dispatch through this protocol.

stdin is closed. stdout and stderr are ordinary byte streams, not JSON and not success indicators. Exit 0 succeeds; nonzero exit fails. Startup errors fail with no exit code. Stop and deadline cancel the entire child process tree, using the platform boundary already needed by plugins. A completed parent must not leave children running. On parent exit or cancellation, cleanup has at most one second; inability to confirm cleanup is reported and prevents a replacement run for that task until explicit recovery. The short card/detail/action deadline remains unchanged: a confirmed Run submits work and returns promptly; it does not execute a long script inside that request.

Reloaded task definitions affect future runs only. The current run keeps its captured command, directory, environment and timeout. Disabling a task stops future starts but lets its active run finish. Removing a task, disabling/trashing the plugin or orderly hostdeck shutdown cancels affected runs and releases timers, file handles and the owner lock before moving the plugin folder. A refused folder move may restore scheduling only after the original identity is verified. Hidden workspace entries keep scheduling; hiding is not disabling.

On restart, records left in `starting` or `running` become `interrupted`, not successful or automatically retried. Their tasks remain runtime-paused until explicit Resume acknowledges the uncertain outcome. An abrupt hostdeck exit can leave a process alive on some systems; recovery must not kill an unverified or reused PID, or claim the old process stopped. Without confirmed cleanup, Resume is refused and the task explains what needs local inspection. Other tasks may resume their future schedules. Ownership and cleanup must be resolved before any replacement process starts.

## Run records and logs

The task ID identifies a configuration entry. Each accepted attempt gets a separate opaque, unique run `id` of 1–128 ASCII letters, digits, underscores or hyphens; skipped scheduled attempts get one too. Configuration `revision` is an opaque identifier for a validated task definition with defaults applied and no resolved environment values. Keep it across restart and unchanged reloads; change it when the effective definition changes. It identifies what was requested without saving another command or secret. Run records have these fields:

| Field | Meaning |
| --- | --- |
| `id`, `task_id`, `revision` | Run identity, task identity and configuration revision. |
| `trigger` | `scheduled` or `manual`. |
| `scheduled_at` | UTC RFC 3339 timestamp, or null for a manual run. |
| `accepted_at` | UTC RFC 3339 timestamp when the attempt was recorded. |
| `started_at`, `finished_at` | UTC RFC 3339 timestamps; null until the relevant event, and `started_at` stays null when no process started. An interrupted run uses the recovery observation time for `finished_at`, not an invented process exit time. |
| `status` | `starting`, `running`, `succeeded`, `failed`, `timed_out`, `cancelled`, `interrupted` or `skipped`. |
| `exit_code` | Integer only when an exit code was observed; otherwise null. Cancellation/timeout status takes precedence even if termination returns a code. |
| `reason` | Null for active and successful runs; stable code for other terminal states: `exit`, `start`, `environment`, `timeout`, `stop`, `task_removed`, `plugin_disabled`, `shutdown`, `recovery`, `cleanup`, `log_io`, `overlap` or `capacity`. Optional diagnostic wording is not a stable code. |
| `logs.stdout`, `logs.stderr` | Relative files below `data/logs/`, or null when no log is available. No absolute paths or `..` components; use the record to locate logs, never a caller-supplied path. |
| `logs.stdout_truncated`, `logs.stderr_truncated` | Booleans saying that the respective captured output exceeded its byte cap. |

Normal transitions are `starting → running → terminal`, or `starting → failed/timed_out/cancelled/interrupted`. `skipped` is terminal with no process or logs. `running` may recover to `interrupted`; terminal records do not return to active states. Preserve observed times on every transition. A stopped, timed-out or cleanup-failed attempt must not be labelled successful.

Each stream keeps its first 1 MiB and continues draining/discarding further bytes so a full log cannot block a child. Display replaces invalid UTF-8 and treats control sequences and HTML as text. A run's known nonempty referenced environment values are redacted from captured logs and surfaced diagnostics, including matches split across writes. This cannot hide secrets transformed or independently obtained by the script; scripts must avoid printing those secrets. Never serialize the effective environment.

History keeps at most 30 days and 1,000 terminal records per plugin. Log files have a shared 64 MiB cap. Prune oldest terminal history/logs first; an active run's bounded logs and start record are never pruned. If old logs expire before their record, set the corresponding path to null. If storage cannot reserve the configured bounded space or persist state, refuse new work and report storage failure. Log I/O failures cancel an affected run and report `failed/log_io` when cleanup is confirmed, otherwise `interrupted/cleanup` and pause the task. Never report a missing durable result as success. The exact task-state serialization is to be specified with its storage implementation; the run-record fields above are the agreed data model.

### Successful run example

The revision below is illustrative; an implementation assigns it to the effective definition.

```json
{
  "id": "run-01",
  "task_id": "daily-backup",
  "revision": "task-revision-01",
  "trigger": "scheduled",
  "scheduled_at": "2026-10-26T18:00:00Z",
  "accepted_at": "2026-10-26T18:00:00Z",
  "started_at": "2026-10-26T18:00:00Z",
  "finished_at": "2026-10-26T18:00:12Z",
  "status": "succeeded",
  "exit_code": 0,
  "reason": null,
  "logs": {
    "stdout": "data/logs/run-01.stdout.log",
    "stderr": "data/logs/run-01.stderr.log",
    "stdout_truncated": false,
    "stderr_truncated": false
  }
}
```

At minimum, a nonzero exit is `failed/exit` with the observed code; a deadline is `timed_out/timeout`; an explicit Stop is `cancelled/stop`; a concurrent scheduled attempt is `skipped/overlap` with null start, exit code and log paths. Cancellation during shutdown is `cancelled/shutdown` when confirmed; an uncertain stop becomes `interrupted/cleanup` and keeps the task paused.

## Controls, trust and implementation acceptance

The detail item ID is the task ID. Run, Pause and Resume use action IDs `run`, `pause` and `resume`; Stop uses `stop:<run-id>`, embedding the active run ID so a stale control cannot stop a later run. The action boundary re-reads and checks that exact offered ID, and the runner checks it again atomically before cancelling. These are plugin-owned action IDs, not new fields in the generic protocol. Run, Pause, Resume and Stop require confirmation through the existing detail-action boundary. Run means accepted rather than completed. Its existing action `message` includes the run ID as text; the next detail reading identifies the run. No new field is added to action-program JSON. Pause/Resume persist runtime preference separately from locally authored `enabled`. Effective permission to run requires both; Resume cannot enable a task whose configuration says false. Disabling/trashing does not erase tasks or history; purging the plugin deletes both. Future mutating HTTP handlers must check the console's Origin and document exactly which files they write in the security model. This document adds none.

Scripts are trusted local code, running with hostdeck's user rights. Working-directory resolution is not a sandbox; scripts can access files and networks available to that user. Command definitions remain locally authored in this version. The browser may display execution information and request the defined controls, but cannot upload scripts, submit command text, set environment values or choose arbitrary paths. Existing plugins, scripts and saved files require no migration for this documentation-only proposal.

Implementation must use outside-in failing tests before each behavior, deterministic clock control and temporary scripts/processes at boundaries. Acceptance includes both configuration examples; invalid/duplicate fields and versions; cron and interval boundaries, DST gaps/repeats and clock jumps; no execution on reads; same-task exclusion and four-slot capacity; manual/scheduled runner parity; nonzero exit, missing executable/environment and timeout; full process-tree cleanup; stale Stop; pause, removal, reload and restart; writer exclusion and corrupt state; output caps/redaction across chunks; history/storage bounds and write failure. macOS and Windows tests must verify their actual execution boundaries; real-browser tests cover both languages and narrow layouts when controls are added. No runtime, UI or operating-system behavior is verified by publishing this contract alone.
