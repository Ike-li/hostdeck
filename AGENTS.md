# hostdeck: notes for coding agents

hostdeck is one Go program serving a console on 127.0.0.1, for macOS and Windows: a sidebar of local web UIs and an overview of plugin cards. Read these before changing behaviour:

- `README.md`: goals, **Not planned**, roadmap, the plugin manifest. Its scope is the owner's to decide.
- `docs/ARCHITECTURE.md`: the packages, what each request does, the **contracts and compatibility**, the **security model**, tests and CI.
- `docs/DESIGN.md`: the design system: tokens, components, patterns and accessibility, with values, reasons and the tests that keep them. A change to the console's look updates it in the same commit.
- `docs/PLUGINS.md`: the plugin contract: the folder, every field of `plugin.yaml`, how plugin programs run and what they print. Tests check that it names every field the manifest parser and the programs' decoders accept.

Each also exists as `*.zh-CN.md`. Change both languages in the same commit.

## Working with the owner

- Reply in 简体中文. Code, comments, commit messages and logs are in English.
- Ask first when two readings of a request would give different results, and before anything hard to undo: a release tag, a force push, deleting any branch but the one just merged, or changing README's goals, Not planned or roadmap beyond what the owner agreed in the conversation.
- Report what was verified, with the command and its key output, and what was not.

## Commands

```sh
go test -short ./...                        # fast: no console build, browser or real-machine reads
HOSTDECK_E2E=1 go test -count=1 ./...        # full acceptance; never reuse an E2E result cache
HOSTDECK_E2E=1 go test -count=1 ./e2e/       # browser suite only; HOSTDECK_BROWSER picks Chrome or Edge
go test -race -short -count=1 . ./internal/... ./web
gofmt -l . ; go vet ./... ; GOOS=windows go vet ./...
go run . --dir "$(mktemp -d)" --addr 127.0.0.1:0   # a throwaway instance; it logs its URL
```

`HOSTDECK_BROWSER` points the browser tests at a browser binary. A throwaway instance keeps the owner's own `~/.config/hostdeck` untouched. Headless Chrome lays pages out at least 500 px wide, so check narrow layouts at 520.

## How work is done

- Work in **slices**: one user-visible behaviour per branch, commit and PR.
- **TDD**, one behaviour per loop: a failing test that states the behaviour from outside (the HTTP API, the browser, the files written), then the smallest implementation that passes, then the next. Test names are sentences, such as `TestAFailedReloadKeepsWhatWasLoadedAndSaysWhy`. Fakes sit at the boundaries only: `httptest` servers, the test binary as a plugin program, temporary folders.
- After green, break the new code once by hand (a **mutation**), watch a test go red, restore it, and record it under `Tested:`.
- The frontend is plain ES modules in `web/`: no build step, no npm. The API answers with error **codes**; the page words them from `web/i18n/en.json` and `web/i18n/zh-CN.json`, which hold the same keys (`web/i18n_test.go` checks).
- Every API call that changes something checks the console's `Origin`. A new one gets a line in the security model saying what it can write.
- The plugin folder and manifest, what plugin programs print, the CLI and the files in hostdeck's folder are **contracts** (ARCHITECTURE, Contracts and compatibility); `/api/*` is not. A change to a contract updates where it is written down, the built-in examples and the agent prompts (`help.agent.*`) in the same commit, and the body says what an existing plugin, script or file needs. From the first release, keep the rules written there.
- Platform code sits behind build tags (`_unix.go`, `_windows.go`). Only CI runs Windows.
- Before hostdeck takes on a plugin's own business, holds its running state, or answers for its exit and recovery, the slice weighs three ways and says in `Rejected:` why two of them lost: widen the public plugin contract, which every plugin gains and every release keeps; give that built-in a channel of its own (routes, a page module, state held in the process); or leave the work to the plugin's own program or to the system. Being useful does not by itself make a feature hostdeck's to carry.

## Commit, PR, merge

The owner approved this flow for every finished slice, without asking each time:

1. `TZ=UTC git commit`. The subject is imperative; the body says what changed and why, then trailers, one per line, each key repeatable:
   - `Constraint:` an outside condition that limited the choice
   - `Rejected: <option> | <why>`
   - `Tested: <what ran> <result>`
   - `Not-tested: <what was not verified>`, required whenever anything was not

   Before pushing, `git log --format='%ad | %cd' --date=iso` shows `+0000` on every new commit.
2. Push the branch; `gh pr create --fill`.
3. Wait until `gh pr checks <n>` shows every check `pass` and none `pending`: `checks (macos-latest)`, `race (windows-latest)`, `unit (windows-latest)`, `browsers 1/4` to `browsers 4/4` on each system, and `archives`. Fix red CI on the branch.
4. Run `git checkout main && git merge --ff-only <branch> && git push origin main` under `set -euo pipefail`, with no pipe after a git command. Once `gh pr view <n> --json state` says `MERGED`, delete the branch locally and on origin. Merge by fast-forward on this machine only, never by GitHub's merge buttons, so the UTC dates and trailers stay as written.

Decisions live in the commit trailers, and `git log` is the design history. Add no decision files.

## Decisions recorded nowhere else

Agreed in the M0 plan, 2026-10-22:

- Releases: GoReleaser builds a draft release on a `v*` tag. No tag until the owner has tried the app and says so.
- A plugin's page is `proxy` or `link`; plugins serve no HTML of their own.
- Secrets are `${env:NAME}` only; a keyring comes with M3.
- A forwarding port belongs to its plugin ID in `ports.json` for good. If another program holds it, that service shows an error; it never moves to another port.
- Chrome and Edge are tested; Firefox and Safari are best effort.
- Size and memory: CI is to report them, and a budget is set after five runs on main.

## Where the work stands (2026-10-29)

This section says where things are now: what is built, what is left, what is known to be wrong. A slice that changes one of these replaces the lines that no longer hold instead of adding a paragraph. What a finished slice did lives in its commit and in README, ARCHITECTURE, DESIGN, PLUGINS and TASKS; `git log -p AGENTS.md` keeps the longer account this section held until 2026-10-29.

Built:

- M0 apart from the release work below: the console, embedding with clear cache and notifications, cards, details and actions, the saved layout, the plugins page with editing, links and the trash, and the CLI with its agent prompts. The layout and CRUD pass and the UI/UX pass are finished, and DESIGN.md writes the design system down.
- M1: background jobs (launchd, Windows services) with run, restart and their logs; every TCP listener, with a confirmed Stop; services started from a shell, with stop and restart; Docker containers, with start and restart.
- M2's first slice: the cards `notifications.json` names are read in the background, and a change two readings in a row have seen runs the user's command with `HOSTDECK_*` variables (README, Notifications).
- Outside the roadmap: disk history, a read-only process list, scheduled script tasks, and service observations, which follow a service's logs and capture a restarted service's output.

Left:

- M0: a release workflow run by a `v*` tag; README notes on Gatekeeper (macOS) and SmartScreen (Windows) for the unsigned program; the size and memory report in CI.
- The rest of M2, in this order: the console saying whether the checks run and when each watched card was last checked; a Watch toggle in a card's More actions that writes only the watch list, with that exception added to the README's Not planned in the same slice (the owner agreed on 2026-10-29); then a webhook and the system's notifications. UDP listeners (M1) and any other new view of this machine wait behind it.
- M3 and M4, as the README's roadmap has them.
- Paused until review, as the README's roadmap says: scheduled script tasks, restarting a service started from a shell, and service observations receive fixes but no new abilities. The review falls on 2026-11-12, two weeks after M2's first slice reached main.

Known problems:

- Never tried by hand: Windows at all, `examples/disk/card.ps1`, dragging a real address from the browser, LAN host names such as `nas.local`, a notification command on Windows, a real push delivery, Firefox and Safari, a real Docker start or restart, and capturing a real service's output after a restart.
- CI failures with no established cause, where a second occurrence is worth a look: `TestOneClickCopiesTheCompletePromptIntoTheBrowserClipboard` read an empty clipboard in Edge on macOS (run 40235433558); the held-layout timer-discovery fixture advanced 10 ms instead of less than 1 ms (run 40761683272); an overview-add navigation timed out in Chrome on Windows (run 40760679303); `TestTheServiceToolbarKeepsThePageWhenItsMorePanelCloses` waited 10 s for a console that had started but neither logged nor exited, in Edge on macOS, after passing in Chrome in the same shard, and passed when rerun (run 40875533692).
- A built-in plugin already installed keeps the files it was copied with until its folder is replaced, so an installed jobs or disk plugin does not receive later script changes by itself.
