# hostdeck: notes for coding agents

hostdeck is one Go program serving a console on 127.0.0.1, for macOS and Windows: a sidebar of local web UIs and an overview of plugin cards. Read these before changing behaviour:

- `README.md`: goals, **Not planned**, roadmap, the plugin manifest. Its scope is the owner's to decide.
- `docs/ARCHITECTURE.md`: the packages, what each request does, the **security model**, tests and CI.

Both also exist as `*.zh-CN.md`. Change both languages in the same commit.

## Working with the owner

- Reply in 简体中文. Code, comments, commit messages and logs are in English.
- Ask first when two readings of a request would give different results, and before anything hard to undo: a release tag, a force push, deleting any branch but the one just merged, or changing README's goals, Not planned or roadmap beyond what the owner agreed in the conversation.
- Report what was verified, with the command and its key output, and what was not.

## Commands

```sh
go test ./...                              # unit tests; browser tests too if Chrome or Edge is found
HOSTDECK_E2E=1 go test -count=1 ./e2e/     # browser tests; a missing browser fails instead of skipping
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
- Platform code sits behind build tags (`_unix.go`, `_windows.go`). Only CI runs Windows.

## Commit, PR, merge

The owner approved this flow for every finished slice, without asking each time:

1. `TZ=UTC git commit`. The subject is imperative; the body says what changed and why, then trailers, one per line, each key repeatable:
   - `Constraint:` an outside condition that limited the choice
   - `Rejected: <option> | <why>`
   - `Tested: <what ran> <result>`
   - `Not-tested: <what was not verified>`, required whenever anything was not

   Before pushing, `git log --format='%ad | %cd' --date=iso` shows `+0000` on every new commit.
2. Push the branch; `gh pr create --fill`.
3. Wait until `gh pr checks <n>` shows every check `pass` and none `pending`: `test (macos-latest)`, `test (windows-latest)`, `archives`. Fix red CI on the branch.
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

## Where the work stands (2026-10-22)

M0 slices S1 to S7 are merged, and after them the plugins page, reloading plugins as that page changes them, and embedding a web page by its address.

The owner asked for a layout and CRUD pass and approved this shape: failures say what is wrong, card numbers stay inside the card, a narrow window uses a top bar, the overview can refresh, cards open, the plugins page groups and sorts by name, reorder works from the keyboard, cards can be hidden and the layout reset, a page's name, icon and address can be edited with yaml.Node on an allowlist, a link can be added, and delete goes through a trash folder. The page does not write a command, and there is no settings page yet. Plain errors is the first of these, card numbers staying inside the card is the second, the narrow window's top bar is the third, and the overview saying when a card was read is the fourth.

Left for S8, the last M0 slice:

- a release workflow run by a `v*` tag;
- README notes on Gatekeeper (macOS) and SmartScreen (Windows) for the unsigned program;
- the size and memory report in CI.

Known problems:

- Never tried by hand: Windows at all, `examples/disk/card.ps1`, dragging a real address from the browser, LAN host names such as `nas.local`.
- An embedded page's host is checked to be local only when it is added; a name that later resolves to the internet is still proxied.

Update this section in the slice that changes it.
