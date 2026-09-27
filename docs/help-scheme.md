# The `help:` URL scheme

`help:` is **not a standalone scheme** — it is mew's single **registered wiki**,
and its backing store is the `box:` tree. The registry entry `wikiRegistry["help"]`
declares a DokuWiki-format wiki whose root is the `box:` URL `box:///help`, so
every `help:/…` reference rewrites into a document under that root. `help:` is,
in effect, a user-facing alias for `box:///help/…`.

See [`mew-scheme.md`](mew-scheme.md) for the backing `box:` VFS; `help:` inherits
its three-layer overlay (a shipped help page backs `help:/start` even with no
`~/.mew/help/start.txt` on disk).

## The mapping

```
help:/<id>   →  box:///help/<id>.txt   →  ~/.mew/help/<id>.txt   (local/OS mode)
help:/       →  the "start" page       →  box:///help/start.txt
help:/a/b    →  box:///help/a/b.txt
```

- `.txt` is optional: `help:/start` and `help:/start.txt` are the same page.
- The authority form `help://start` is accepted (leading slashes are trimmed).
- Bare `help:foo` (no slash) is **intentionally not a scheme** — it stays an
  ordinary wiki namespace reference, not a `help:` link (`TestKeysRefAction`
  in `0_keyverbose_test.go` rejects `help:keys#x`).
- `help:` is deliberately **absent** from the generic `linkSchemes` table
  (`http/https/ftp/file/box/mew`: the `linkSchemes` map in `wikiref.go`). A registered wiki name is
  recognized *before*, and separately from, generic external schemes.

## Subsystems that use the scheme

Roughly seven subsystems touch `help:`.

| # | Subsystem | Role | Where to look |
|---|---|---|---|
| 1 | Registry / definition — `internal/editor/wikiref.go` | `wikiRegistry["help"]`: `Root: box:///help`, `Ext: .txt`, `Start: start`, `Writable: true`, docked-top ToolViewport titled "Help", `DeclineExec`; the `wikiDef` type | `wikiRegistry` and the `wikiDef` type in `wikiref.go` |
| 2 | Parse → resolve → map — `wikiref.go` + `internal/editor/canon.go` | `wikiSchemeRef` recognizes `help:/…`; `resolveFollow` → `resolveInWiki` canonicalizes `box:///help` and runs the DokuWiki id pipeline to `box:///help/<id>.txt` | `wikiSchemeRef`, `resolveFollow` and `resolveInWiki` (with its `matchWikiPath`) in `wikiref.go`; `canonicalDocURL` in `canon.go` |
| 3 | Link-follow & docked help navigation — `internal/editor/links.go` + `internal/editor/help.go` | Following `help:` links; the single docked help slot; Quick-Help-vs-manual; back/forward history | `followLinkSpan` and `openWikiScheme` in `links.go`; `toggleHelp`, `openHelp`, `showHelpLocation` and `quickHelpDestination` in `help.go` |
| 4 | CLI / launch / Open / keybinding — `cli.go`, `buffers.go`, conf, app `host.go` | `mew help:/start`; `buffer_open_file "help:/"`; `^B H = help_toggle "help:/"`; the "Using mew" menu item | `openLaunchFile` in `cli.go`; `openFile` in `buffers.go`; the `^B H` line in `resources/defaults/keys_buffer_and_save_menus.conf`; the `mew.help.usingmew` item in `buildMenus` (`app/internal/mewhost/host.go`) |
| 5 | Syntax-format tie-in — `internal/editor/syntaxhl.go` | A buffer whose URL lies within a wiki's root highlights as that wiki's `Format`, so pages under `box:///help` render as **dokuwiki** | the registered-wiki branch of `bufferGrammar` in `syntaxhl.go` |
| 6 | Modebar display + exec policy — `internal/plugins/modebar.go`, `wikiref.go` | Reverse-maps a help buffer URL back to the `help:/start` form for the modebar; `DeclineExec` refuses a terminal inside a help viewport | `SetFilenameFunc` on the modebar plugin, which the editor points at `wikiDisplayName` (`links.go`); the registry entry's `DeclineExec` field, read by `wikiDeclineExec` in `wikiref.go` and consulted by `execRequestSpecPolicy` (`pty.go`) and `runFilter` (`filter.go`) |
| 7 | Prose specs — `docs/` | The `help:` / `goto:` / `mark:` / `url:` / `cmd:` link vocabulary + the rule that content-derived links may carry only navigation schemes | `docs/hyperlink-ideas.md`, section "2. Targets: PawScript as the universal action"; `docs/dokuwiki-ids.md`, section "Layer 1 — mew scheme / interwiki gate" |

## The wiki registry

`help` is the sole entry in `wikiRegistry` today (hardcoded pending a
config-driven registry — see the comment on `wikiRegistry` in `wikiref.go`). Its
`wikiDef` declares:

| Field | Value |
|---|---|
| `Name` | `help` (the scheme name) |
| `Format` | `dokuwiki` |
| `Root` | `box:///help` |
| `Ext` | `.txt` |
| `Start` | `start` (so `help:/` = `help:/start`) |
| `Writable` | `true` (create-on-miss via `wikiCreateURL` in `wikiref.go`) |
| `DeclineExec` | refuses spawning a terminal in the help viewport |
| viewport | `ToolViewport`, `DockTop`, `ViewportSet: help`, `Title: Help`, height 4–20 |

A wiki viewport carries `WikiRoot` (canonicalized `box:///help`) and `WikiName`
(`help`) fields, set wherever `links.go` opens a wiki page: `followLinkSpan`,
`openWikiScheme` and `promptCreatePage`.

## Navigation: Quick Help vs the manual

The docked help slot (`helpViewportTag = "help"`, top dock; these
constants open `help.go`) carries **two classes**:

- **Manual** (`helpViewportClass = "help"`) — a real `help:/…` wiki page under
  `box:///help`.
- **Quick Help** (`quickHelpClass = "quickhelp"`) — a *synthetic* buffer at
  **`mew:/quickhelp`**, placed deliberately **outside** `box:///help` so it
  never resolves as a wiki page. It is the WordStar-style context reference, and
  is **not** the `help:` scheme (`quickHelpDocURL` in `help.go`).

The two are one navigable slot: `help_toggle` / `help_open` →
`toggleHelp` / `openHelp` in `help.go`. `openHelp` prepends `help:/`
to a bare argument, so `help_toggle "keys"` and
`help_toggle "help:/"` both work. Quick Help can itself forward to real pages:
`quickHelpDestination` builds `ref := "help:/" + topic`,
e.g. `help:/keys`. `showHelpLocation` → `swapBuffer` builds the viewport's own
back/forward history so the reader returns where they came from
(`showHelpLocation` in `help.go`, `swapBuffer` in `links.go`); `closeHelpViewport` restores document focus.

## CLI and launch

- `mew help:/start` — `openLaunchFile` in `cli.go` routes a wiki-scheme
  operand through `openWikiScheme` so the real page loads and roots the
  viewport, rather than a blank OS open. The help readout does not become the
  primary buffer; an empty editing area opens beneath it.
- `openFile` routes wiki schemes through `openWikiScheme` too
  (`openFile` in `buffers.go`), so `buffer_open_file "help:/"` opens the index
  (`buffer_open_file` is registered in `registerBufferCommands`, also in
  `buffers.go`).
- Key binding `^B H = help_toggle "help:/"`
  (the `^B H` line in `resources/defaults/keys_buffer_and_save_menus.conf`).
- App/host menu: `mew.help.usingmew` → `help_toggle "help:/"` (the "Using mew"
  index item in `buildMenus`, `app/internal/mewhost/host.go`); `mew.help.quickhelp` → bare `help_toggle`
  (Quick Help, not the scheme).

## Storage-path safety

`normalizeDocPath` leaves `help:/…` untouched as a scheme path so it is not
absolutized into `<cwd>/help:/start.txt` (`normalizeDocPath` in `canon.go`;
`TestNormalizeDocPathSchemesPassThrough` in `0_canon_normalizedocpath_test.go`).

## Not the scheme (excluded)

Internal identifiers and plain help text that share the word "help" but are not
URLs: the `help_toggle` / `help_open` command names, `helpViewportTag="help"`,
`ViewportSet:"help"`, the `Title:"Help"` bar, `helpViewportClass="help"`; the
`mew.help.*` dotted app-command IDs (the Help menu in `buildMenus`, `app/internal/mewhost/host.go`); and CLI `--help`,
`printUsage`, `showHelp`, and help-text prose.

**Quick Help** (`mew:/quickhelp`, `[quickhelp::colors]`) is a near-match: it is
its own `mew:` buffer, not the `help:` scheme — it becomes scheme-relevant only
when it forwards to a `help:/<topic>` page.
