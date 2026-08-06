# KittyTK upstream sync patch

**STATUS: LANDED upstream in KittyTK `v0.1.3-alpha`** (commits `c6854ac` "Apply
downstream mew sync patch: cursive Arabic, script-aware fonts, TUI cipher" and
`d835c71` "Use genuine Adobe Source Han Serif SC…", build bumped to 3). mew's
vendored `./kittytk` is now re-synced to that release: fonts, `fonts.go`, and
`core/version.go` adopted from the tag, so the only remaining vendored↔upstream
divergence is the mew boundary below. `app/go.mod` pins
`github.com/phroun/kittytk v0.1.3-alpha`. This directory is kept as the
development record.

**UPDATE — LANDED upstream in KittyTK `v0.1.7-alpha`**: the next round of fork
improvements went up as three themed PRs, all merged, and mew's `./kittytk` is
re-synced to `v0.1.7-alpha` (`app/go.mod` + `go.mod` pins bumped):

- [#15](https://github.com/phroun/kittytk/pull/15) — TUI backend: terminal
  restore, RTL drift mode, Hebrew precompose (`hebrew` pkg), combining width.
- [#16](https://github.com/phroun/kittytk/pull/16) — Graphical PurfecTerm:
  pixel-precise mouse (`?1016`), script-aware Arabic, embedded focus, OSC-52.
  See `PIXEL-MOUSE.md` + `pixel-mouse.patch` (both now historical).
- [#17](https://github.com/phroun/kittytk/pull/17) — Window & menu chrome:
  resize-hover, tear-off pixel drag, menu roles/anchors, pass-next-key/F10,
  cursor subtree, host-terminal detection (`hostterm` pkg).

Dependency bumps carried by those PRs: `purfecterm v0.2.27 → v0.2.30`,
`direct-key-handler v0.3.9 → v0.3.11`. After this sync, the only
vendored↔upstream divergence is the mew boundary (the 19 `//go:build mew`
files + go.mod's mew require) — see `docs/kittytk-subtree.md`.

**LANDED upstream in KittyTK `v0.1.8-alpha`** (PR
[#19](https://github.com/phroun/kittytk/pull/19)) — `sdl-sethint-preload.patch`
(`sdl/sdl3/sdl3.go`, against v0.1.7): `Platform.Run` calls
`sdl3.SetHint("SDL_APP_NAME", …)` BEFORE
`sdl3.Init` when an app name is set (`SDL_APP_NAME` must precede `SDL_Init` to
take effect). In the purego binding, every entry point — `SetHint` included —
dereferences a function pointer that stays nil until `loadLibrary` opens
libSDL3, which today happens only inside `Init`. So the pre-`Init` `SetHint`
segfaults. The standalone `kittytk-sdl` never sets an app name, so `p.appName`
is empty and the call is skipped; **mew-sdl is the first caller** (it sets
`SetAppName("mew")`), which is what surfaced the latent bug. Fix: `SetHint`
loads the library first (idempotent — it shares `Init`'s `libLoaded` guard), so
a pre-`Init` hint works and `SDL_APP_NAME` still lands before `SDL_Init`.
Applied to the vendored tree to unblock mew-sdl, then sent upstream as PR
[#19](https://github.com/phroun/kittytk/pull/19) and released in `v0.1.8-alpha`
(build 7 → 8). mew's `./kittytk` is re-synced to that tag; the vendored
`sdl3.go` was already byte-identical, so the sync only bumped the build counter
and the `go.mod` pins. This directory keeps the patch as the development record.

**LANDED upstream in KittyTK `v0.1.9-alpha`** (PR [#21](https://github.com/phroun/kittytk/pull/21)) — `solo-tearoff-reentrancy.patch` (`objects/trinkets/
desktop.go` + `desktop_tearoff.go` + a regression test, against v0.1.8): opening
a dialog while the desktop is in **solo mode** (the mew default — the root
window torn off onto its own OS surface) produced TWO window instances that
both responded to hover. `Desktop.createTornHost` latches its "claimed" state
(`wm.RemoveWindow` / `SetDetached`) only AFTER `platform.CreateSurface`, and on
the SDL backend creating that OS window fires a `WindowResized` event-watch that
**drains the post queue synchronously** — re-running the deferred
`soloAdoptWindow` for the very window being torn, while its guards still read
"not yet torn". The window was then hosted on two surfaces at once. Fix: a
`tearing` claim set marked at the top of `createTornHost` (before
`CreateSurface`), so a re-entrant tear of the same window is a no-op. This is a
KittyTK-general window-management bug (any solo host, any dialog), not
mew-specific. `desktop_tearoff_reentrant_test.go` reproduces the re-entrancy
with a fake platform and asserts a single host (it fails without the guard —
three surfaces).

**LANDED upstream in KittyTK `v0.1.9-alpha`** (PR [#21](https://github.com/phroun/kittytk/pull/21)) — `about-rotation-gate.patch` (`sdl/platform_sdl.go` +
`objects/trinkets/desktop.go` + a test, against v0.1.8): the WebGPU rotation
easter egg was triggered by the **R key globally** — pressing `r` anywhere (in
the editor!) toggled a full-window spin, and the key still fell through to be
typed. It also auto-started from the macOS application-menu About item, which is
the system menu, not the desktop's own About box. Gate it: the R-key toggle now
fires only while a `rotationGate` predicate is true, and the desktop wires that
to `aboutBoxFocused` — the built-in "About KittyTK" dialog being open and
active — and the key is consumed when it fires. The stray auto-start on the
mac About item is removed. So the egg is reachable only from the About KittyTK
box, and `r` is an ordinary key everywhere else. The gate is offered to the
platform through an anonymous interface, so the renderer-agnostic trinkets stay
free of an SDL dependency. `desktop_aboutrotation_test.go` locks the gate
(false with no dialog / after focus leaves / after close; true only while the
box is open and active).

**LANDED upstream in KittyTK `v0.1.15-alpha`** (PR
[#29](https://github.com/phroun/kittytk/pull/29)) — `purfecterm-grid-cap.patch` (`objects/trinkets/purfecterm.go` +
`purfecterm_gfx.go` + `purfecterm_gridcap_test.go`, against v0.1.14): a
`clampGridDim`/`maxTermGridDim` (2000) backstop on both fit paths
(`updateTerminalSize`, `paintGraphical`). A degenerate fit — a near-zero cell
size mid-resize, or a size-feedback runaway between a hosted terminal and its
host — drove the grid to hundreds of thousands of cells, which the emulator
allocated (one `makeEmptyLine` per row) into a multi-gigabyte buffer that
OOM-killed the process. The cap collapses a negative/degenerate value to 0
(callers skip the resize) and never touches a legitimate fit. Independent of any
single runaway's root cause; carried in the vendored tree at `Build 15` ahead of
release, so the resync bumps only the pins when v0.1.15 is cut. `TestClampGridDim`
locks the boundaries. (The newer subtree records — #25/#26/#27/#28 and this one —
live in full in `docs/kittytk-subtree.md`; this log's detailed narrative predates
them.)

**LANDED upstream in KittyTK `v0.1.16-alpha`** (PR
[#30](https://github.com/phroun/kittytk/pull/30)) — `solo-restore-zerosize-sdl2.patch` (against v0.1.15): three
graphical-host fixes plus the SDL3-migration cleanup, all shared files.
(1) **Solo maximize→restore** reconfigures the swapchain: in solo mode the app
IS the primary OS window (RESIZABLE, border stripped at runtime), and restoring
it from zoom-to-fill left the GPU swapchain at the maximized size — restored
content painted into the top-left corner until a manual edge-drag.
`sdlSurface.SetScreenSizePx` now clears an OS `MAXIMIZED` flag before resizing
and drives the framebuffer reconfigure from the window's real pixel size
(`Window.SizeInPixels()`) rather than waiting on a `WINDOW_RESIZED` a
programmatic resize didn't deliver; adds `sdl3.WINDOW_MAXIMIZED`. (2)
**Zero-size compositing** no longer panics (`NewRGBA … huge or negative
dimensions`): dragging the desktop to ~0 height with a child/overlay open made
`hostPixels/hostUnits` divide by a unit extent that rounds to 0 → `+Inf` →
`MaxInt64`; `RenderFrameWithChildWindows` and `drawOverlay` now guard the
divisor (the frame-level scale already did). (3) **SDL2 stragglers** dropped:
`buildwin.sh` rewritten from the SDL2/CGO/MinGW static-link cross-build to a
plain purego `GOOS=windows go build` with `sdlembed`, and comments that still
called the running system SDL2 corrected to SDL3 (`compositing-handoff.md` kept
— it narrates the migration itself). `Build 16`. Verified on macOS (webgpu) +
standalone build/vet/tests with no mew module. **LANDED in v0.1.16.**

`kittytk-sync.patch` brought upstream KittyTK (`github.com/phroun/kittytk`,
developed against main @ `27e64de`) up to date with the improvements developed
in mew's vendored fork (`mew/kittytk`), minus everything that properly belongs
to mew itself. Verified: applied clean to `27e64de`, and the patched tree built
and passed its FULL test suite standalone (`GOWORK=off go build ./... &&
go test ./...`) with no mew module anywhere in the graph.

Note on the release fonts: upstream re-sourced the embedded fonts from
`FONTS.md` rather than copying mew's exact bytes, so the release's Arabic/Serif
builds differ byte-for-byte from the ones originally verified on-screen — but
they are valid joining builds (`TestEmbeddedArabicFacesJoin` passes on the
release tree) and mew has adopted them for consistency. The genuine Adobe
Source Han Serif SC (`78aa7a32…`, adobe-fonts/source-han-serif @ tag `2.003R`)
replaces mew's earlier byte-twin.

## What it achieves

53 files, +4664/−246, plus 12 embedded font binaries (see "Applying"). The
headline areas:

1. **Cursive Arabic in the PurfecTerm gfx renderer** — the centerpiece. Cells
   holding Arabic (base letters or the presentation forms bidi-aware apps
   emit) are joined for real: each cell shapes a window of prev + tatweels +
   letter + tatweels + next as ONE run so the font's GSUB produces true
   contextual forms, then keeps an exactly-cell-wide slice centred on the
   letter whose cut ends land mid-stroke, so adjacent cells meet at their
   boundaries. Includes the presentation-form→base reverse map (standard
   Unicode Forms-A/B data), the Unicode joining-type classifier, and one-time
   stderr diagnostics (`arabic face=… join=…` / `arabic geom=…`) that report
   the resolved face, join verdict, and slice geometry from a live run.
2. **Embedded font set that actually shapes** — Noto Naskh + Kufi Arabic
   swapped to the archive (phase-2 hinted) builds: current Noto Arabic
   releases implement the dotted "tooth" letters via chained-contextual GSUB
   that go-text/typesetting does not execute, so runs shaped with them leave
   the middle letters ISOLATED (`TestEmbeddedArabicFacesJoin` locks the
   requirement to the embedded faces). Also adds Noto Serif (4 styles), Noto
   Serif Hebrew, and Sans/Serif CJK SC.
3. **Systematic script-aware font tree** — `ui-{text,term}-{western,hebrew,
   arabic,cjk}-{sans,serif}` aliases with per-glyph script-class resolution;
   script-classed runes resolve to their script face BEFORE the primary, so a
   Latin-centric primary with incidental coverage of a few script codepoints
   can neither render wrong isolated forms nor split the shaping run.
   `RuneSpanX` cluster-span queries on shaped paragraphs. Font loading from
   files/dirs (`fontload.go`), `SetFontAlias` chains, engine epochs.
4. **PurfecTerm renderer parity with the wire protocol** — per-cell font slots
   (SGR 10–20 / OSC 7004), script-class fonts (OSC 7005), VTFRAKTUR (SGR 20),
   mouse reporting/visual fixes, exact cell-rect mask sizing at fractional
   pixels-per-unit (mask box now uses the same edge math as cell-rect fills).
5. **TUI cipher pseudo-fonts** — Unicode Mathematical-Alphanumeric ciphering
   for bold/italic/fraktur on plain terminals, with `[tui]` hostcfg gating and
   an independent `fraktur_mode`.
6. **Host config + fixes** — `[fonts]`/`fonts_path`/`ui_*` alias overrides in
   kittytk.ini wired into both hosts; SGR 20 in style; editor-trinket host
   seams (`SetLaunchArgv`, `SetShowDesktop/HideDesktop`) on the placeholder;
   window manager/tearoff fixes; macOS About-menu hook (`sdl/aboutmenu_*`);
   `examples/editordemo`; Makefile fix (upstream's `-tags mew` build cannot
   compile upstream, where the mew editor file does not exist).

Regression tests ride along at the layer that earned them: the Arabic render
tests feed BOTH base letters and presentation forms and assert identical
masks, and an end-to-end test paints through the real raster backend and
asserts the joined baseline on the actual framebuffer.

## Applying

From the upstream kittytk checkout at `27e64de`:

    git apply kittytk-sync.patch
    cp <mew>/kittytk/text/fonts/{NotoKufiArabic-*.ttf,NotoNaskhArabic-*.ttf,\
NotoSerif-*.ttf,NotoSerifHebrew-*.ttf,NotoSansCJKsc-Regular.otf,\
NotoSerifCJKsc-Regular.otf} text/fonts/
    GOWORK=off go build ./... && GOWORK=off go test ./...

The 12 font binaries (~43 MB) are shipped as a copy step rather than inflating
this patch; their bytes live in `mew/kittytk/text/fonts/`. Per-file provenance
— exact versions, sha256 hashes, and verified external source URLs (all on
raw.githubusercontent.com) — is in `FONTS.md`. For a single self-contained
artifact instead, run in the patched tree:
`git add -A && git diff --staged --binary > full.patch`.

Note: the patch's `go.mod`/`go.sum` carry NO `github.com/phroun/mew`
requirement (the vendored tree needs it only for the build-tagged
`editor_mew.go`, which this patch excludes; the upstream module graph stays
mew-free — verified zero references after apply).

## Deliberately excluded (the mew boundary)

- `objects/trinkets/editor_mew.go`, `editor_protocol_mew.go` — the mew-backed
  editor (`//go:build mew`, imports `github.com/phroun/mew`). Upstream keeps
  the placeholder; the patch extends the placeholder's contract surface only.
- `core/version.go` — upstream's Build counter (already ahead); bump on merge.
- `README.md` — upstream's license/support additions are newer; untouched.
- `garland/` — actively developed upstream; untouched (mew consumes the
  separate `github.com/phroun/garland` module instead).

After applying, the ONLY divergence between upstream and mew's vendored tree
is that list — verified by full-tree diff.

## Judgment calls to review

- The one-time Arabic stderr diagnostics are unconditional (one line per
  process, only when Arabic renders). They earned their keep; gate them if
  upstream prefers silence.
- Comments name mew as the reference consumer (matching upstream's existing
  editor-contract voice) but no longer reference mew internals.
