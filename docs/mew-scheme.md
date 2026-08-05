# The `box:` URL scheme (mew's storage)

> This scheme was spelled `mew:` until the box:// split. `box:` is now mew's
> **storage** scheme (config, grammars, help pages — file-backed, overridable);
> mew's **generated** surfaces (Quick Help) moved to a separate `mew:` scheme.
> See [`mew-scheme-overlay.md`](mew-scheme-overlay.md) for how both sit on the
> KittyTK `profile://`/`box://` model.

`box:` is mew's own **storage scheme** for its support/config tree — the space
that used to be spelled `~/.mew/`, mew's own *box*. It is deliberately separate
from `file:///`, which addresses ordinary documents on disk. A `box:` URL names
a resource *mew owns* (its config, grammars, help, crash dumps), independent of
where — or whether — that resource lives on the real filesystem.

## Spelling and confinement

- mew always **emits** the empty-authority form `box:///rel` (three slashes) —
  "this app's own box".
- On **input** the parser also accepts `box:x`, `box:/x`, and `box://x`; all
  canonicalize to the single identity `box:///` + confined path.
- The authority slot (`box://<authority>/…`) names another Application's box in
  the KittyTK model; mew resolves only its own (empty authority) today.
- The tree is **confined**: a `..` can never rise above the root. Paths are
  cleaned as `path.Clean("/" + rel)`, so `/a/../../b` → `/b` and `/..` → `/`.

Two chokepoints own the whole surface: `internal/editor/mewfs.go` (resolution)
and `internal/editor/canon.go` (identity). Change how `box:` resolves in those
two places and the rest of the editor follows.

## Host modes

The scheme is backed one of two ways, chosen at construction:

- **Local (default):** `box:///x` maps to `<home>/.mew/x`, where `<home>` is
  `WithHomeDir` (default the OS user home). Reads fall through three layers —
  user copy → system resource dirs → embedded resources — while writes only
  ever touch the user layer.
- **Virtualized:** `WithMewFileSystem` supplies a `FileSystem`, and each
  `box:///x` is handed to it verbatim (scheme intact). A `box:///` document may
  then have no local path at all. Confinement is preserved either way.

## Subsystems that use the scheme

Nine subsystems genuinely address `box:` (plus the generated `mew:` scheme).

| # | Subsystem | Role | Key anchors |
|---|---|---|---|
| 1 | Scheme spec / VFS resolver — `internal/editor/mewfs.go` | Authoritative prose spec; the `mewVFS` overlay (user → system → embedded); `isBoxPath`, `confine()`, `makeConfigFileIO` | `mewfs.go:12-46`, `93-104`, `49-218`, `365-391` |
| 2 | Config manager — `internal/config/config.go` | `box:///editor.conf` root; the `FileIO` scheme contract; default `box:///` → `~/.mew`; include confinement | `config.go:630-722`, `660-667`, `748-768` |
| 3 | Canonical doc identity — `internal/editor/canon.go` | Normalizes every spelling to one `box:///` identity; folds `box:///help/x` ↔ real `~/.mew/help/x` in OS mode | `canon.go:14-70`, `209-258` |
| 4 | Wiki / link navigation — `internal/editor/wikiref.go` (+ `links.go`, `internal/viewport/manager.go`) | `"box"` is a followable link scheme; help root `box:///help`; `WikiRoot` may be `box:///docs` | `wikiref.go:50-56`, `130-153`, `390-425`; `manager.go:357-361` |
| 5 | Syntax grammars — `internal/editor/syntaxhl.go` | `box:///syntax/<name>.jsf` resolved through the layered tree | `syntaxhl.go:170`, `476-482` |
| 6 | Embedded / system resources — `internal/editor/resources.go` | `//go:embed resources` (syntax, help, default confs) as the lowest read layers | `resources.go:14-73` |
| 7 | Editor core & commands — `internal/editor/editor.go` | profile/deadcat wiring; `mew:/quickhelp`; screen-dump `box:///<ts>.ans` | `editor.go:934-939`, `8729`, `2928` + `7751` |
| 8 | Save / exists safety — `internal/editor/sourcesafety.go` | `box:`-target saves route through `e.mew.WriteFile`; dir-create prompts skipped | `sourcesafety.go:341-350`, `163-166` |
| 9 | Public embedding API — `mew.go` (+ CLI launch `internal/editor/cli.go`) | `WithMewFileSystem` / `WithHomeDir` / `WithDeadcat` govern the backing | `mew.go:108-119`, `487-491`; `cli.go:322,333` |

## Content that lives under the scheme

| Resource | Canonical URL | Anchor |
|---|---|---|
| Editor config + includes | `box:///editor.conf` | `config.go:664`, `984`, `999` |
| Startup profile script | `box:///profile.mew` | `config.go:1798-1824`; `editor.go:8340-8356` |
| Syntax grammars | `box:///syntax/<name>.jsf` | `syntaxhl.go:170` |
| Help manual (wiki) — see [`help-scheme.md`](help-scheme.md) | `box:///help/…` | `wikiref.go:130-153` |
| Quick Help (synthetic) | `mew:/quickhelp` | `editor.go:8729` |
| Screen-capture debug dumps | `box:///<timestamp>.ans` | `editor.go:2928`, `7751` |
| Embedded / system resources | (lowest read layers) | `resources.go:14-73` |
| Crash dumps (DEADCAT) | conceptually in-tree — see note below | `mew.go:109`; `editor.go:646-648`, `2922` |

## Resolution layers (local mode)

A `box:` read consults three layers in order; the first hit wins:

1. **User layer** — `<home>/.mew/<rel>` (the only layer writes touch).
2. **System resource dirs** — from `[storage] resources=`
   (`config.go:568-586`), resolved by `systemResourceDirs` in `resources.go`.
3. **Embedded resources** — the `//go:embed resources` tree
   (`resources.go:14-73`), injected via `config.SetEmbeddedResources`.

`mewVFS` (`mewfs.go:49-218`) implements `ReadFile` / `WriteFile` / `Stat` /
`IsDir` / `Glob` over these layers, and `LocalPath` / `relForLocal` /
`fallbackForLocal` bridge a real `~/.mew/...` path back to the fallback layers.

## Config includes under the scheme

`@include` resolution stays inside the scheme (`config.go:731-855`):

- **Quoted** `@include "..."` resolves relative to the *including* file
  (`joinInclude` / `includeDir`), clamped so a leading `../` can never rise
  above `box:///`.
- **Angle** `@include <...>` resolves against the config root `box:///`.

## Document identity and navigation

- `canon.go` `canonicalDocURL` (`:42-70`) maps `box:x` / `box:/x` / `box://x` /
  `box:///x` to one identity; in OS-backed mode a `box:` name resolves to the
  **real** `~/.mew` `file://` identity, so `box:///help/start.txt` and
  `~/.mew/help/start.txt` are the same buffer.
- `wikiref.go` treats `box` (paired with `file`) as a followable document
  scheme; `docStat` / `docList` (`:429-502`) dispatch `box://` reads and globs
  through `e.mew`.
- `cli.go:322,333` handle `mew help:/start` on the launch walk and guard
  `SetFilename` normalization with `isBoxPath`.

## Looks like a usage, but isn't

- **`internal/editor/deadcat.go`** writes crash dumps to **real** OS paths
  (`filepath.Join(e.home, ".mew", …)`), not literal `box:` strings — even
  though the tree is *documented* as part of the scheme. `[storage] deadcat=`
  can override the location (`config.go:568-572`, `1342`).
- **`app/internal/mewhost/hostconf.go:17`** mentions the `box:/` sandbox only to
  say it deliberately **bypasses** it: the launcher reads host settings straight
  from OS `~/.mew/editor.conf` (`:67`).

## Not the scheme (excluded)

The `fmt.Fprintf(os.Stderr, "mew: …")` program-name prefixes are ordinary CLI
messages, not URLs: `app/cmd/mew/{main_plain,main_kittytk,install}.go` and
`app/internal/selfinstall/*`.
