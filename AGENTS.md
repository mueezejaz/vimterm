# AGENTS.md

Windows-only Vim-like terminal emulator: spawns a shell in a ConPTY, renders its output through a built-in VT emulator, and drives it with modal keybindings. Module `vimterm`, Go 1.26+ (per go.mod), stdlib + charmbracelet/x ConPTY stack.

## Commands

 - Build: `go build -o vimterm.exe .`
- Test all: `go test ./...` (~50 s; `internal/app` alone ~12 s)
- Single test: `go test ./internal/app -run TestPasteWithCount`
- Formatting: plain `gofmt`; no linter is configured, but gofmt-clean matters (see commit history).

There is no CI test workflow — `.github/workflows/release.yml` only builds/releases on `v*` tags — so nothing validates your change unless you run tests locally.

## Testing quirks

- Several `internal/app` tests are integration tests that spawn real shells (`powershell.exe`, `cmd.exe`) through ConPTY; they need Windows 10 1809+ and account for most of the suite's runtime.
- Unit tests elsewhere construct components directly: tests fake the `session` interface (internal/app/app.go) and override `App.clipRead`/`clipWrite` instead of touching the real clipboard.
- `internal/emulator` has no direct tests; its behavior is covered indirectly by `internal/app` tests feeding bytes into `emu.Write`. Add emulator coverage there.
- `zz_bugverify_test.go` files are temporary bug-repro tests marked for removal after review — don't extend or imitate them.

## Architecture

Flow: `main.go` -> `app.Run` (internal/app/app.go) wires console input events -> keybind engine -> action map -> PTY session writes; PTY output -> emulator (VT parsing) -> renderer drawing frames on a dirty flag.

- Search, selection, and cursor positions use absolute buffer lines = scrollback lines + live screen lines (`emu.ScrollbackCell(x, n)` vs `emu.Cell(x, n-sbLen)`). Mixing these up is a classic bug source.
- Geometry: the child gets hostRows-1 rows normally (one row reserved for the status line); full height while the child is in the alternate screen (status merging).
- `pty.Spawn` wraps the shell command in a `chcp 65001` UTF-8 codepage wrapper on Windows to prevent mojibake. Don't bypass it when spawning shells; `cmd/dumpbytes` exists to hex-dump raw ConPTY bytes when debugging encoding issues.

## Concurrency invariants

- The config hot-reloader calls `applyConfig` from a watcher goroutine once per second. New config-derived state must be guarded by `cfgMu` and read through accessor methods, never read directly off `App`. It must not iterate `a.tabs` (main-loop-owned slice); background tabs pick up config changes on activation via the `t.sb` cache in `applyChildRows`.
- Tabs: the focused tab's state is materialized into the App fields (`sess`, `emu`, `vp`, `search`, `cur`, `sel`, ...) and mirrored back into `tabs[active]` by `storeCurrent` before another tab loads (`internal/app/tab.go`). Only the main loop may switch/close tabs or mutate the slice; reader/waiter goroutines capture their session's `sess`/`emu`/`gen` at start. Exited sessions are reaped on the frame tick (`reapTabs`).
- Any background goroutine that can panic must defer `restoreOnPanic`: a crash must restore the host terminal out of raw mode.

## Commits

Lowercase imperative with a scope prefix matching the touched package(s): `app: fix ...`, `config: ...`; multiple scopes comma-separated (`app,search: ...`).
