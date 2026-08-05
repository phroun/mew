# The `help:` URL scheme

`help:` is **not a standalone scheme** — it is mew's single **registered wiki**,
and its backing store is the `mew:` tree. The registry entry `wikiRegistry["help"]`
declares a DokuWiki-format wiki whose root is the `mew:` URL `mew:///help`, so
every `help:/…` reference rewrites into a document under that root. `help:` is,
in effect, a user-facing alias for `mew:///help/…`.

See [`mew-scheme.md`](mew-scheme.md) for the backing `mew:` VFS; `help:` inherits
its three-layer overlay (a shipped help page backs `help:/start` even with no
`~/.mew/help/start.txt` on disk).

## The mapping

```
help:/<id>   →  mew:///help/<id>.txt   →  ~/.mew/help/<id>.txt   (local/OS mode)
help:/       →  the "start" page       →  mew:///help/start.txt
help:/a/b    →  mew:///help/a/b.txt
```

- `.txt` is optional: `help:/start` and `help:/start.txt` are the same page.
- The authority form `help://start` is accepted (leading slashes are trimmed).
- Bare `help:foo` (no slash) is **intentionally not a scheme** — it stays an
  ordinary wiki namespace reference, not a `help:` link (`keybadge_test.go:27`
  rejects `help:keys#x`).
- `help:` is deliberately **absent** from the generic `linkSchemes` table
  (`http/https/ftp/file/mew`, `wikiref.go:50-56`). A registered wiki name is
  recognized *before*, and separately from, generic external schemes.

## Subsystems that use the scheme

Roughly seven subsystems touch `help:`.

| # | Subsystem | Role | Key anchors |
|---|---|---|---|
| 1 | Registry / definition — `internal/editor/wikiref.go` | `wikiRegistry["help"]`: `Root: mew:///help`, `Ext: .txt`, `Start: start`, `Writable: true`, docked-top ToolViewport titled "Help", `DeclineExec`; the `wikiDef` type | `wikiref.go:130-154`, `79-124` |
| 2 | Parse → resolve → map — `wikiref.go` + `internal/editor/canon.go` | `wikiSchemeRef` recognizes `help:/…`; `resolveFollow` → `resolveInWiki` canonicalizes `mew:///help` and runs the DokuWiki id pipeline to `mew:///help/<id>.txt` | `wikiref.go:184-194`, `532-553`, `659-671`; `canon.go:216-236` |
| 3 | Link-follow & docked help navigation — `internal/editor/links.go` + `internal/editor/editor.go` | Following `help:` links; the single docked help slot; Quick-Help-vs-manual; back/forward history | `links.go:295-347`, `374-444`; `editor.go:8708-9037` |
| 4 | CLI / launch / Open / keybinding — `cli.go`, `editor.go`, conf, app `host.go` | `mew help:/start`; `buffer_open_file "help:/"`; `^B H = help_toggle "help:/"`; the "Using mew" menu item | `cli.go:314-327`, `editor.go:6722-6730`; `resources/defaults/keys_buffer_and_save_menus.conf:9`; `app/internal/mewhost/host.go:571` |
| 5 | Syntax-format tie-in — `internal/editor/syntaxhl.go` | A buffer whose URL lies within a wiki's root highlights as that wiki's `Format`, so pages under `mew:///help` render as **dokuwiki** | `syntaxhl.go:476-489` |
| 6 | Modebar display + exec policy — `internal/plugins/modebar.go`, `wikiref.go` | Reverse-maps a help buffer URL back to the `help:/start` form for the modebar; `DeclineExec` refuses a terminal inside a help viewport | `modebar.go:48`, `links.go:349-372`; `wikiref.go:146`, `171-176` |
| 7 | Prose specs — `docs/` | The `help:` / `goto:` / `mark:` / `url:` / `cmd:` link vocabulary + the rule that content-derived links may carry only navigation schemes | `docs/hyperlink-ideas.md:43-57`, `docs/dokuwiki-ids.md:74-76` |

## The wiki registry

`help` is the sole entry in `wikiRegistry` today (hardcoded pending a
config-driven registry — `wikiref.go:126-129`). Its `wikiDef` (`wikiref.go:79-124`)
declares:

| Field | Value |
|---|---|
| `Name` | `help` (the scheme name) |
| `Format` | `dokuwiki` |
| `Root` | `mew:///help` |
| `Ext` | `.txt` |
| `Start` | `start` (so `help:/` = `help:/start`) |
| `Writable` | `true` (create-on-miss via `wikiCreateURL`, `wikiref.go:677-686`) |
| `DeclineExec` | refuses spawning a terminal in the help viewport |
| viewport | `ToolViewport`, `DockTop`, `ViewportSet: help`, `Title: Help`, height 4–20 |

A wiki viewport carries `WikiRoot` (canonicalized `mew:///help`) and `WikiName`
(`help`) fields (`links.go:323-324`, `410-412`).

## Navigation: Quick Help vs the manual

The docked help slot (`helpViewportTag = "help"`, top dock,
`editor.go:8708-8724`) carries **two classes**:

- **Manual** (`helpViewportClass = "help"`) — a real `help:/…` wiki page under
  `mew:///help`.
- **Quick Help** (`quickHelpClass = "quickhelp"`) — a *synthetic* buffer at
  **`mew:///quickhelp`**, placed deliberately **outside** `mew:///help` so it
  never resolves as a wiki page. It is the WordStar-style context reference, and
  is **not** the `help:` scheme (`editor.go:8726-8729`).

The two are one navigable slot: `help_toggle` / `help_open` →
`toggleHelp` / `openHelp` (`editor.go:8789-8890`). `openHelp` prepends `help:/`
to a bare argument (`editor.go:8854-8857`), so `help_toggle "keys"` and
`help_toggle "help:/"` both work. Quick Help can itself forward to real pages:
`quickHelpDestination` builds `ref := "help:/" + topic` (`editor.go:9014-9037`),
e.g. `help:/keys`. `showHelpLocation` → `swapBuffer` builds the viewport's own
back/forward history so the reader returns where they came from
(`editor.go:8925-8945`); `closeHelpViewport` restores document focus.

## CLI and launch

- `mew help:/start` — `openLaunchFile` (`cli.go:314-327`) routes a wiki-scheme
  operand through `openWikiScheme` so the real page loads and roots the
  viewport, rather than a blank OS open. The help readout does not become the
  primary buffer; an empty editing area opens beneath it.
- `openFile` routes wiki schemes through `openWikiScheme` too
  (`editor.go:6722-6730`), so `buffer_open_file "help:/"` opens the index
  (`editor.go:2817`).
- Key binding `^B H = help_toggle "help:/"`
  (`resources/defaults/keys_buffer_and_save_menus.conf:9`).
- App/host menu: `mew.help.usingmew` → `help_toggle "help:/"` (the "Using mew"
  index item, `host.go:571`); `mew.help.quickhelp` → bare `help_toggle`
  (Quick Help, not the scheme).

## Storage-path safety

`normalizeDocPath` leaves `help:/…` untouched as a scheme path so it is not
absolutized into `<cwd>/help:/start.txt` (`canon.go:184-207`;
`normalizedocpath_test.go:10-17`).

## Not the scheme (excluded)

Internal identifiers and plain help text that share the word "help" but are not
URLs: the `help_toggle` / `help_open` command names, `helpViewportTag="help"`,
`ViewportSet:"help"`, the `Title:"Help"` bar, `helpViewportClass="help"`; the
`mew.help.*` dotted app-command IDs (`host.go:569-576`); and CLI `--help`,
`printUsage`, `showHelp`, and help-text prose.

**Quick Help** (`mew:///quickhelp`, `[quickhelp::colors]`) is a near-match: it is
its own `mew:` buffer, not the `help:` scheme — it becomes scheme-relevant only
when it forwards to a `help:/<topic>` page.
