# KittyTK — Session Handoff

Continuity document for resuming development in a fresh session. The GitHub
repo is being renamed (module path is already `github.com/phroun/kittytk`, so
the rename brings the repo name into line with the module — no import-path
changes needed).

> **Post-rename access check (confirmed):** after the repo rename, git
> fetch and push from this session still succeed. The local `origin` remote
> still reads the old path (`.../git/phroun/tuitk`) but resolves correctly —
> the session proxy targets a stable repo identity and GitHub redirects the
> old name. No remote reconfiguration was needed to keep pushing to
> `claude/project-survey-mr6k4v`. (A human clone should still update its
> `origin` URL to the new name at leisure; GitHub's redirect covers it
> either way.) This file covers the toolkit only; the `garland/` subtree is
a separate self-contained module (`github.com/phroun/garland`) mirrored here
for development and synced back to its own repo — it has its own docs under
`garland/docs/`.

## What KittyTK is

A client/server UI toolkit in Go with two render backends behind one
protocol:

- **TUI backend** (`cmd/kittytk-tui`) — terminal cells.
- **SDL backend** (`cmd/kittytk-sdl`, build tag `sdl`) — graphical, with
  proportional text, pixel-exact rendering, multi-surface windows.

Apps are **clients** that connect over "kittytalk" and drive the display
server with a declarative window/trinket protocol. `cmd/demoapp` is the
reference client (9-tab main window, menus, dialogs, terminals). A C-side
`ptydriver/` runs terminal child processes client-side. Client libraries
exist for **Go** (`client/`), **Python** (`python/`), and **C** (`c/`).

## Directory map

| Dir | Purpose |
|---|---|
| `protocol/` | Wire protocol: window/trinket properties, generic collection, introspection (descriptor registry + wire query) |
| `objects/` | Trinkets (widgets): buttons, tabs, splitters, ListView, TreeView (multicolumn), TextInput, ComboBox, ScrollArea, MDIPane, WindowManager, PurfecTerm terminal, menus |
| `display/` | Desktop/session composition, window management, solo mode, tear-off |
| `layout/` | Layout engine (denomination cells, fixedbox, splits) |
| `style/` | Palettes incl. `termpalette.go` (dark/light/active), fonts |
| `sdl/`, `platform/`, `backend/` | Render backends and platform glue |
| `client/`, `python/`, `c/` | App-side client libraries (Go / Python / C) |
| `core/`, `text/` | Shared primitives, text measurement/advance |
| `hostcfg/` | Host auth stores (allow/deny, known hosts) |
| `ptydriver/` | C pty driver for client-side terminal processes |
| `garland/` | Rope text-buffer library (separate module — see its own docs) |

## Verification gates (run before every commit)

```
go build ./...
go build -tags sdl ./...
go test ./...
go test -tags sdl ./objects/...
```

Visual work is proven with PNG captures from the SDL backend when relevant.

## Git conventions used this whole project

- Branch: work on the designated `claude/...` branch; push with
  `git push -u origin <branch>`.
- Run `git add`/`commit` from the **repo root** (running `git add garland/...`
  with cwd inside `garland/` fails — recurring gotcha).
- Commit messages end with the session's Co-Authored-By / Claude-Session
  trailers; never put model IDs in commits or code.

## Architecture decisions that matter (accumulated rulings)

- **font_size is fractional pixels-per-unit** (not integer points). All
  chrome — titlebars, buttons `[x][.][^]`, progress bars, tabs, splitter
  grab dots, clock (80% scale) — derives from it. Window default sizes and
  fixedbox widths are **denomination-aware** (sized in denomination cells).
- **Text advance is pixel-offset based** (caret/menu/status measured, not
  cell-multiplied). PurfecTerm's graphical interior is **native-pixel**
  (own viewport + font px, cell derived from exact font pixel metrics).
- **Tear-off windows**: any window can detach to its own OS surface (SDL
  multi-surface), with per-window tear-off trait, `%`/`#` title handle,
  drag undock/redock, black stroke indicator, detached chrome (menu bar +
  status bar on the torn window). Desktop refocus never steals focus from
  a detached window.
- **Solo mode**: client handshake flag (`client.DialSolo`); main window
  tears off and the primary surface closes; extra windows auto-tear; app
  quits when the last torn window closes.
- **kittytalk endpoints**: `unix:`/`tcp:`/`tls:` schemes across all three
  client languages. Go host does mTLS with interactive/callback
  authorization (Yes/Always/No/Never/BlockHost), persistent allow/deny
  stores, and token bypass; clients keep persistent identity certs and
  TOFU host pinning. Host-side approval dialog renders on the desktop.
- **Protocol introspection**: descriptor registry queryable over the wire.
- **TreeView multicolumn** (most recent work): Go model + API, TUI and GUI
  paint paths, generic collection protocol, pixel-drawn tree lines,
  `treelines` option, column fit-drag, pinned-right flank, edge fades,
  peek row, scroll re-clamping on resize. Latest fix: no tree scrolling
  while a choice editor's drop-down is open.
- **Palette system**: raster palette and PurfecTerm scheme both driven
  from `style/` ActiveTermPalette (dark/light/active).

## Open items (the remaining docket)

1. **layoutwidth/layoutheight window properties** (#98) — protocol +
   window properties to declare window size in layout units, resolved
   against the current scale. Follow-on from the denomination-aware
   sizing work. Small/medium.
2. **Pixel-exact hit-testing at fractional font sizes** (#105, optional) —
   rendering is pixel-exact at fractional scales but mouse→cell mapping
   may still round through integer cell math; clicks near boundaries can
   land one cell off at odd sizes. Polish.
3. **Unify MDI/desktop window management** (#106) — `MDIPane` and
   `WindowManager` duplicate drag/clamp/corral/minimize-dock logic.
   Refactor `MDIPane` to compose `WindowManager`. Largest item; pure
   refactor with regression risk in MDI interactions (dock restore,
   2-column drag limit, provisional corral are all pinned by tests).

Confirm scope against current code before starting any of these — the
descriptions are from the running task list, not a fresh audit.

## Known long-horizon ideas (not scheduled)

- Editor trinket built on garland (see `garland/docs/` for the buffer's
  capabilities: in-place & lock-free save, integrity triage/rebase,
  ephemeral cursors, decorations-as-marks; syntax highlighting sketch in
  `garland/docs/syntax-highlighting-ideas.md` — ruling: sparse checkpoint
  state marks, not one per line).
