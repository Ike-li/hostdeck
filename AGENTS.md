# hostdeck: notes for coding agents

hostdeck is one Go program serving a console on 127.0.0.1, for macOS and Windows: a sidebar of local web UIs and an overview of plugin cards. Read these before changing behaviour:

- `README.md`: goals, **Not planned**, roadmap, the plugin manifest. Its scope is the owner's to decide.
- `docs/ARCHITECTURE.md`: the packages, what each request does, the **security model**, tests and CI.
- `docs/DESIGN.md`: the design system: tokens, components, patterns and accessibility, with values, reasons and the tests that keep them. A change to the console's look updates it in the same commit.

Both also exist as `*.zh-CN.md`. Change both languages in the same commit.

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

## Where the work stands (2026-10-25)

M0 slices S1 to S8 are merged, and after them the plugins page, reloading plugins as that page changes them, and embedding a web page by its address.

The owner asked for a layout and CRUD pass and approved this shape: failures say what is wrong, card numbers stay inside the card, a narrow window uses a top bar, the overview can refresh, cards open, the plugins page groups and sorts by name, reorder works from the keyboard, cards can be hidden and the layout reset, a page's name, icon and address can be edited with yaml.Node on an allowlist, a link can be added, and delete goes through a trash folder. The page does not write a command, and there is no settings page yet. Plain errors is the first of these, card numbers staying inside the card is the second, the narrow window's top bar is the third, the overview saying when a card was read is the fourth, a card opening its plugin's page is the fifth, the plugins page grouping and sorting by name is the sixth, reordering from the keyboard is the seventh, hiding a plugin or resetting the layout is the eighth, editing a page's name, icon or address is the ninth, adding a link is the tenth, and moving a plugin to the trash and purging it from there is the eleventh.

The UI/UX pass has started: navigation now has compact rows, More actions panels, shared 44px controls, and a narrow-screen drawer with a backdrop and keyboard focus management. Overview now has compact cards, More actions panels, refresh feedback and actionable empty states. A reading waits while card controls have focus, and failed layout saves restore the server state. Plugin management now has labelled forms, a direct add shortcut and More actions panels for infrequent changes. The service toolbar now keeps cache clearing in More actions, reports progress outside the panel and retains the embedded page during console interaction. The UI/UX pass is complete: details separate values, fields, bounded scrollable logs and actions, show written statuses, and fit narrow windows. Server contracts and stored files retain their formats. Dragging console links or dropping an address on the console's own origin no longer adds a plugin; external service address drops and reordering still work. The layout-saved notice and its undo expire after eight idle seconds following a successful save, pausing for pointer or keyboard interaction; undo and failed saves dismiss it immediately. An always-visible plus card at the end of the overview opens the existing page/link/built-in-plugin add area, including from an empty overview, without entering the saved layout or plugin sorting. The sidebar now aligns the brand and navigation on a 24px icon grid, gives fixed entries visible icons, centres status and action columns, highlights entire active rows, and groups management with a segmented language switch in the footer.

Management now uses the Pages & plugins entry and shows Enabled as its first main section, followed by Disabled, Needs attention and Add content. Every enabled, visible connection appears in both sidebar and overview: card-only plugins open a generated monitor view, and connections without monitoring have destination shortcut cards. Invalid manifests retain error entries; explicit hiding and disabling still apply. More actions opens above its trigger when the window has insufficient room below, preserving access to primary row actions. Uniform rows distinguish declared features, enablement and workspace visibility. Hide from workspace is neutral, Restore to workspace is explicit, and enabling preserves hidden preferences without controlling external services. The plugin directory is secondary disclosure content. Editing now uses a native modal dialog with labelled allowlisted fields, full descriptions, mode guidance, Cancel/Save, focus containment and restoration. Refused saves keep the draft and show a local error; successful saves close and announce on the updated row. Long content scrolls above visible actions. Built-in plugins now use full-width responsive rows sharing the management identity and capability presentation, with complete setup instructions and right-aligned Add actions; confirmation behavior is unchanged.

Connection help now explains web page, built-in plugin and custom plugin setup in both languages. Separate one-click agent prompts include the current console origin, absolute plugin directory, shell and PowerShell commands, manifest/JSON contracts (including `card.exec.command`, `detail.exec.command`, `actions.exec.command`, `exec.windows` and optional detail item fields), page ID derivation (`data.id`), `page update` flags, temporary backup cleanup and verification for both pages/links and card/detail plugins; clipboard failure selects a read-only preview. Native page/plugin CLI commands now support page add/update, full catalog listing, built-in or validated local installation, enable/disable and explicit reload. The CLI emits JSON with exit codes 0/1/2 and uses the selected loopback console; custom files are copied locally, never submitted through HTTP. Prompts invoke the running executable’s absolute path. Manual file changes can use plugin reload without restarting; fatal reload failures retain loaded state and post-install failures report saved files. The reload endpoint checks Origin and may update ports.json through forwarding synchronization, without writing plugin commands. Copying help writes no data. Disabled plugins can be edited in place without enabling them, empty names are rejected, non-ASCII hostnames fall back to `page` folder IDs, embedded page proxies verify local-network addresses when dialing, declarative HTTP cards refuse redirects and report oversized responses, and detail actions serialize their re-read and execution. An empty sidebar and an overview with no cards now say what to do next where the entries would be, a first reading of the overview that never arrived says so there too instead of leaving the grid silent, and every error code the Go side answers with is worded in both languages. What the console did last is announced once, with the urgency it deserves - a result as a status, a failure as an alert - and what cannot be seen, such as how many cards there are or how many entries a search matched, is said by one live region outside every view, so no rebuild can reach it. The keyboard now reaches the content from the first Tab, through a skip link that is the page's first element and stays out of the flow. The page is now read as landmarks and one heading ladder: the sidebar is a single named navigation inside a named complementary, the open entry carries `aria-current`, each page is a region named by its own heading, the service view names itself with a real `h1`, and management rows no longer skip a level. (This is the UI/UX pass's S8; the "Left for S8" list below is the M0 slice of the same number.)

The UI/UX pass closed with S8: a skip link reaches the main content from the first Tab, and the page now carries one set of landmarks (a single navigation landmark, regions labelled by their headings) and one heading ladder that runs from a single h1 without skipping a level; popovers, external links and code samples name what they are.

Overview rows now leave room for long names beside long status text, keeping both readable inside the card. A browser fixture checks both languages at 1280, 2964 and 520px. The overview and the plugins page fill the window's width with no maximum, as homedeck does. The overview puts at most four cards in a row, as homedeck did, so a wide window widens cards instead of adding a fifth; narrower windows keep their 220px columns.

Status pills and an armed button under the pointer now keep their words at 4.5:1 in both themes. A stylesheet test measures every word written on a tint, mixed as the browser mixes it over the surface it sits on.
Field edges and a status dot nothing has been heard from now stand out 3:1 from their surfaces, through `--line-control`; the focus ring and the other dots already did, and a stylesheet test keeps all of them there.
A sidebar dot whose address did not answer is now a red ring rather than a disc, so up and down differ by shape as well as colour.
A disabled button no longer changes its edge or fill under the pointer.
The editor's title, the plugins page's subheadings, the help's headings and the plus card's title are now semibold like every other heading, instead of the browser's bold.
Stylesheet tests now keep colours inside the theme's `:root` blocks and radii, weights and easing on their tokens; the unused exit curve is gone.
`docs/DESIGN.md` and its Chinese twin now write the design system down from the shipped console: tokens, components, patterns, accessibility and how to extend it, each rule with its value, its reason and the test that keeps it. The one item it leaves reserved is tying a server error to its field, which waits for an error whose words cannot name the field. TestOneClickCopiesTheCompletePromptIntoTheBrowserClipboard once read an empty clipboard in Edge on macOS CI (run 40235433558) and passed when rerun; the cause is unknown, and a second occurrence is worth a look.

M1 has started. The built-in jobs plugin lists this user's loaded launchd jobs, or on Windows the services whose program is outside the Windows directory. The overview counts how many need attention, including warnings as well as errors on Windows. The detail page can restart a running one or run one that is not. Whole job names must pass the ASCII allowlist, including trailing newlines. Windows actions require a startable service; Restart additionally requires a running or paused state and AcceptStop, so disabled or transitional services cannot be stopped through Restart. Windows program paths are expanded and normalized before filtering; unresolved paths are excluded, and the shared PowerShell helper keeps UTF-8 state text intact. The system domain is not listed. Failed or malformed launchd listings and failed individual reads report bilingual errors without invented counts or actions. Never-run jobs are idle; terminating signals remain visible and count as failures while the job is not running. The launchd JSON encoder preserves control characters and Unicode safely. Which process listens on which port, and Docker, are still ahead.

Browser assertions now verify their triggering events, jobs UI fixtures require concrete rows and actions, and Windows jobs scripts have deterministic query and action coverage. Test fixtures release blocked readers on failure and concurrent-action tests have bounded waits. Windows jobs action errors bypass success-output suppression so failures are reported instead of appearing successful.

Browser suites now share one process while disposing an isolated browser context per case. Cookies, storage, HTTP cache, service workers and permissions are checked across consecutive visits to the same origin. Missing auto-discovered browsers may skip; configured or broken browsers fail, and startup logs the actual executable and version. Console processes and data directories remain per-case. Drawer keyboard checks wait for the initial workspace and open animation, and assert their starting focus before pressing Tab.

Timer-heavy browser tests now advance a network-aware DevTools virtual clock, including exact notice and confirmation boundaries. Layout-notice cases wait for the first card reading before freezing the browser scheduler, rather than treating sidebar visibility as completed startup. Waiting for a timer lets the paused clock run a microsecond at a time, because paused virtual time holds back a promise resolution Blink defers, which had left the layout-notice case waiting for a save's answer on slower runners. A budget that does not finish within ten seconds now fails naming the requests still open and whether the page still answers; one such stall in TestTheLayoutSavedNoticeWaitsWhileUndoHasFocusOrThePointerIsOverIt on Windows is still unexplained and should name its cause when it recurs. Product durations stay unchanged. Real-clock notice expiry, browser cache clearing/history restoration, motion and the backend pending-card boundary remain covered; the slow-card server is released by a gate.

Fast checks now use go test -short ./... without building the console, starting browsers or reading real machine jobs/disks. Full acceptance explicitly disables the Go test-result cache because the child-built executable is outside the E2E dependency graph. CI selects Chrome and Edge explicitly on both systems, runs core race checks, keeps per-test JSON timings and failure HTML/PNG/console artifacts, bounds runs and cancels superseded PR checks. It runs for pull requests, nightly on main and on demand, not on pushes to main. Each system splits the browser suite into four shards that each run every fourth test in Chrome and then Edge; macOS keeps gofmt, vet, race and the other tests in a fifth job, the most macOS jobs an account runs at once.

The overview now offers Reset layout only for nonempty saved layout preferences. Visibility follows successful loads/saves, undo and failed-save recovery without rebuilding cards; a focused reset control moves focus to Refresh when it disappears. Both languages and 1280/768/520px are covered.

Add content now starts with the web-page form and built-in plugins, followed by the complete connection-help disclosure with a quieter top divider. Both add entry points still reveal and focus the address input; keyboard order follows the direct actions before help. Copy prompts, manual-copy fallback and the separate plugin-directory disclosure keep their existing behavior.

The independent disk-history plugin now runs inside hostdeck on macOS and Windows without Python, a separate collector service or another port. Installation and reads do not scan. Confirmed detail actions scan the current user's home folder or up to eight locally configured roots, start or pause hourly tracking, or cancel a scan. History and tracking state stay in the installed plugin's data folder and resume on restart; disabling or trashing cancels scans and timers. Readings count the disk space regular files occupy (a sparse disk image by the blocks it holds, a hard-linked file once on macOS) and never compare it with older file-length readings, skip links, other volumes and the plugin's own files, and compare readings that missed the same unreadable entries (macOS privacy-protected folders, counted on the card) while withholding changes when the missed entries differ, instead of inventing deletions. The overview shows the first root's change and top movers in homedeck's wording ("过去 24 小时 · 主目录共 …", rows such as ~/code), says how to see changes after a single reading, and warns about an old reading only while hourly tracking is on; the existing detail view shows bounded directory summaries and recent history under Log. Storage is bounded to 30 days, 744 snapshots per target, 256 summaries per snapshot and 64 MiB total. Corrupt data refuses writes, process-held OS locks exclude second writers, and failed or cancelled scans retain prior snapshots. Temporary-tree and real-browser fixtures cover native installation, incremental readings, tracking, history retention, Origin refusal and both languages at 1280 and 520px. The old disk-space plugin remains separate. Old homedeck SQLite history is not imported.

Remaining M0 work, outside the UI/UX pass:

- a release workflow run by a `v*` tag;
- README notes on Gatekeeper (macOS) and SmartScreen (Windows) for the unsigned program;
- the size and memory report in CI.

Known problems:

- Never tried by hand: Windows at all, `examples/disk/card.ps1`, dragging a real address from the browser, LAN host names such as `nas.local`.

Update this section in the slice that changes it.
