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
| 1 | Scheme spec / VFS resolver — `internal/editor/mewfs.go` | Authoritative prose spec; the `mewVFS` overlay (user → system → embedded); `isBoxPath`, `confine()`, `makeConfigFileIO` | the file's opening comment (the prose spec); `isBoxPath` and `confine`; the `mewVFS` type and its methods; `makeConfigFileIO` — all in `mewfs.go` |
| 2 | Config manager — `internal/config/config.go` | `box:///editor.conf` root; the `FileIO` scheme contract; default `box:///` → `~/.mew`; include confinement | the `FileIO` type, and `Manager.configPath` as `NewManager` sets it; `boxToLocal`; `joinInclude` / `includeDir` |
| 3 | Canonical doc identity — `internal/editor/canon.go` | Normalizes every spelling to one `box:///` identity; folds `box:///help/x` ↔ real `~/.mew/help/x` in OS mode | `canonicalDocURL` (its `osBackedFS` branch does the fold) and `canonicalOSFileURL` |
| 4 | Wiki / link navigation — `internal/editor/wikiref.go` (+ `links.go`, `internal/viewport/manager.go`) | `"box"` is a followable link scheme; help root `box:///help`; `WikiRoot` may be `box:///docs` | `linkSchemes`, `wikiRegistry`, and the URL path helpers (`urlSplit`, `urlDir`, `urlJoin`, `urlWithin`) in `wikiref.go`; the `WikiRoot` field of `Viewport` in `manager.go` |
| 5 | Syntax grammars — `internal/editor/syntaxhl.go` | `box:///syntax/<name>.jsf` resolved through the layered tree | `resolveSyntaxFile`, and the registered-wiki branch of `bufferGrammar` |
| 6 | Embedded / system resources — `internal/editor/resources.go` | `//go:embed resources` (syntax, help, default confs) as the lowest read layers | `embeddedResources` (the `//go:embed resources` tree) and its `readEmbeddedResource` / `statEmbeddedResource` / `listEmbeddedResource` accessors |
| 7 | Editor core & commands — `internal/editor/editor.go`, `help.go`, `frame.go` | profile/deadcat wiring; `mew:/quickhelp`; screen-dump `box:///<ts>.ans` | `New` builds the resolver (`newMewVFS`) and wires config, profile and DEADCAT (`resolveDeadcat`) through it; `quickHelpDocURL` in `help.go`; `debug_screen` (in `registerScreenCommands`) arms `pendingScreenCapture`, which `performRender` writes — both in `frame.go` |
| 8 | Save / exists safety — `internal/editor/sourcesafety.go` | `box:`-target saves route through `e.mew.WriteFile`; dir-create prompts skipped | `performSave` (a `box:` target writes through `e.mew`) and `missingSaveDir` (no directory prompt for `box:`) |
| 9 | Public embedding API — `mew.go` (+ CLI launch `internal/editor/cli.go`) | `WithMewFileSystem` / `WithHomeDir` / `WithDeadcat` govern the backing | `WithMewFileSystem`, `WithHomeDir` and `WithDeadcat` in `mew.go`; `openLaunchFile` in `cli.go` |

## Content that lives under the scheme

| Resource | Canonical URL | Anchor |
|---|---|---|
| Editor config + includes | `box:///editor.conf` | `NewManager` sets `configPath`; `Manager.Load` and `expandIncludes` read it and its includes |
| Startup profile script | `box:///profile.mew` | `Manager.ProfilePath` in `config.go`; `runProfileScript` in `editor.go` |
| Syntax grammars | `box:///syntax/<name>.jsf` | `resolveSyntaxFile` in `syntaxhl.go` |
| Help manual (wiki) — see [`help-scheme.md`](help-scheme.md) | `box:///help/…` | `wikiRegistry` in `wikiref.go` |
| Quick Help (synthetic) | `mew:/quickhelp` | `quickHelpDocURL` in `help.go` |
| Screen-capture debug dumps | `box:///<timestamp>.ans` | `debug_screen` in `registerScreenCommands`, written by `performRender` (`frame.go`) |
| Embedded / system resources | (lowest read layers) | `embeddedResources` in `resources.go` |
| Crash dumps (DEADCAT) | conceptually in-tree — see note below | `WithMewFileSystem` in `mew.go`; the `DeadcatName` field of `Config` in `editor.go`; `registerDeadcatCommands` in `deadcat.go` |

## Resolution layers (local mode)

A `box:` read consults three layers in order; the first hit wins:

1. **User layer** — `<home>/.mew/<rel>` (the only layer writes touch).
2. **System resource dirs** — from `[storage] resources=`
   (the `Resources` field of `StorageConfig` in `config.go`), resolved by `systemResourceDirs` in `resources.go`.
3. **Embedded resources** — the `//go:embed resources` tree
   (`embeddedResources` in `resources.go`), injected via `config.SetEmbeddedResources`.

`mewVFS` (in `mewfs.go`) implements `ReadFile` / `WriteFile` / `Stat` /
`IsDir` / `Glob` over these layers, and `LocalPath` / `relForLocal` /
`fallbackForLocal` bridge a real `~/.mew/...` path back to the fallback layers.

## Config includes under the scheme

`@include` resolution stays inside the scheme (`readInclude` and `expandIncludes` in `config.go`):

- **Quoted** `@include "..."` resolves relative to the *including* file
  (`joinInclude` / `includeDir`), clamped so a leading `../` can never rise
  above `box:///`.
- **Angle** `@include <...>` resolves against the config root `box:///`.

## Document identity and navigation

- `canon.go` `canonicalDocURL` maps `box:x` / `box:/x` / `box://x` /
  `box:///x` to one identity; in OS-backed mode a `box:` name resolves to the
  **real** `~/.mew` `file://` identity, so `box:///help/start.txt` and
  `~/.mew/help/start.txt` are the same buffer.
- `wikiref.go` treats `box` (paired with `file`) as a followable document
  scheme; `docStat` / `docList` dispatch `box://` reads and globs
  through `e.mew`.
- `openLaunchFile` in `cli.go` handles `mew help:/start` on the launch walk and guards
  `SetFilename` normalization with `isBoxPath`.

## Looks like a usage, but isn't

- **`internal/editor/deadcat.go`** writes crash dumps to **real** OS paths
  (`filepath.Join(e.home, ".mew", …)`), not literal `box:` strings — even
  though the tree is *documented* as part of the scheme. `[storage] deadcat=`
  can override the location (the `Deadcat` field of `StorageConfig`, read from `[storage]` in
  `Manager.applyLayer`).
- **`app/internal/mewhost/hostconf.go`**'s opening comment mentions the `box:/` sandbox only to
  say it deliberately **bypasses** it: the launcher reads host settings straight
  from OS `~/.mew/editor.conf` (`LoadHostConfig`, via `editorConfName`).

## Not the scheme (excluded)

The `fmt.Fprintf(os.Stderr, "mew: …")` program-name prefixes are ordinary CLI
messages, not URLs: `app/cmd/mew/{main_plain,main_kittytk,install}.go` and
`app/internal/selfinstall/*`.
