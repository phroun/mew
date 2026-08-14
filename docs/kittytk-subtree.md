# Syncing `kittytk/` with upstream

`kittytk/` is a **git subtree** of [phroun/kittytk](https://github.com/phroun/kittytk),
not a vendored snapshot. That is deliberate: it makes the fork boundary
structural instead of a list of `--exclude` flags somebody has to remember.
See `kittytk/docs/fork-sync-policy.md` for upstream's side of the contract.

Remote (add once per clone):

    git remote add kittytk-upstream https://github.com/phroun/kittytk

## Pull upstream changes down

    git fetch kittytk-upstream --tags
    git subtree pull --prefix=kittytk kittytk-upstream <tag> --squash

## Send our changes up

    git subtree split --prefix=kittytk -b kittytk-sync
    git diff --stat v<tag> kittytk-sync      # review before pushing

Push `kittytk-sync` to a fork of phroun/kittytk and open a PR.

**Before pushing, drop our fork-only files from the split branch** — they must
never reach upstream, because mew's licence is more restrictive than the
KittyTK base:

    objects/trinkets/editor_mew*.go          (all //go:build mew — 18 files, source + tests)
    objects/trinkets/editor_protocol_mew.go
    go.mod / go.sum      (the github.com/phroun/mew require and its deps)

The fork-only set has grown well past the three files this list used to name.
As of the **v0.1.7-alpha** sync it is **19 Go files**: every `editor_mew*.go`
(the //go:build mew editor implementation and its tests) plus
`editor_protocol_mew.go`. Regenerate the exact list any time with:

    grep -rIl '//go:build mew' kittytk/objects

Upstream ships the complementary `//go:build !mew` placeholders
(`editor.go`, `editor_protocol.go`) implementing the same contract. Changes to
THOSE are fine — encouraged, even, since they keep both ends of the contract in
step.

The invariant upstream holds us to:

> Upstream's tree must build and test with no mew module anywhere in the graph.

## What the subtree bought us

Before the conversion, a sync meant `diff -ruN` of our tree against an upstream
tarball. That describes "make upstream look like my working copy" — and our
working copy has a different boundary, so the diff proposed deleting
`garland/**` (84 files), `patches/**`, upstream's README funding lines and
`core/version.go`, plus sweeping in build binaries and `__pycache__`.

A split of our tree differs from upstream by exactly the fork-only files above
(19 Go files as of v0.1.7) plus the go.mod mew require — zero deletions. The
deletions cannot be proposed because upstream's content simply sits where
upstream put it.

### The v0.1.21 sync (record)

v0.1.20-alpha -> **v0.1.21-alpha**, the desktop-chrome arc as PR
[#35](https://github.com/phroun/kittytk/pull/35) (two commits): resize edges
with an honest grab rule, then the desktop's own themed chrome, one title-bar
kit, and three measured performance fixes.

The delivery is worth recording for **how it nearly went wrong**, twice.

**The stale-note trap.** A note carried across a context compaction said PR
#35's branch still held a condemned affordance — code that lit the resize band
while the pointer sat in what it GUESSED was the OS's resize strip, promising a
resize this program cannot deliver over territory it does not control. Checking
the PR, the head commit's **author date** (21:34) sat *before* the commit that
introduced the affordance (21:44), which read as proof the note was stale — so
the note was "corrected" and the plan changed. It was the correction that was
wrong: the squashed commit had been **amended** at 21:45 and kept its original
author date. The branch did contain the condemned code.

What caught it was not re-reading the PR but `git apply --check` **refusing**:
the delta computed from the wrong base did not apply. Diffing candidate bases
against the branch content then showed `ab1b1c3`'s subtree byte-identical to
it, which pinned the true base and put the revert back in the delivery. **A
commit's author date says nothing about when its content was fixed.** Compare
trees, not timestamps — and treat a patch that won't apply as evidence about
the base, not an obstacle to force past.

**The 9.5x nobody would have seen.** Window chrome had just started painting
content, children and the title bar through the frame's rounded clip, so a
status bar could not square off the corner it sat in. Correct, and invisible in
every test. But the raster backend stood every fast path down on `hasRoundClip`
alone — cached-glyph blits and both alpha composites fell to per-pixel
visibility loops — so a clip that carves four small corners was charged for the
whole surface: **7.4ms vs 0.78ms** on a busy 800x600 scene. `roundClipCovers`
answers in six comparisons whether a rect lies in the uncut interior; the
residual is 1.27x. It surfaced only because a performance pass was run *before*
upstreaming rather than after, and only because the pass **measured** instead of
reading the diff for suspicious-looking code. The other two finds (a font face
allocated per metrics call; the title ellipsis measuring once per trimmed
character, 36us and 148 allocations per title per frame) were the same story.

Pin-only on our side, and unusually cleanly so: the vendored `Build` had already
been run **ahead to 21** when the PR was opened (the v0.1.14/v0.1.15 precedent),
so at tag time every shared file was byte-identical including `core/version.go`
— the resync was the two `go.mod` pins v0.1.20-alpha -> v0.1.21-alpha and
`go.sum` per module via `GOWORK=off go mod tidy`. No dependency change. Running
the counter ahead at delivery time is worth repeating: it makes the resync a
pin bump with nothing to reconcile.

`go mod tidy` per module again left `kittytk/go.mod`'s `garland` indirect alone
(v0.1.11) — it is `go work sync` that wrongly bumps it, as the v0.1.10/v0.1.13
records note.

**New shared interfaces**, all called out in the cover note so upstream could
sweep for unimplemented test doubles: optional platform capabilities
`NativeZoomReporter`, `NativeShapeSquarer`, `NativeRectSetter`,
`NativeMinimumSizer`; `window.TitleControlsInsetProvider`; `core.SetTitleBarScale`
/ `TitleBarScale`; and a `TitleBarScale` field on `hostcfg.Config`. All but the
last two are opt-in type assertions, so an implementation lacking them keeps
working and the feature simply stands down.

The fork boundary is unchanged: the **25 `//go:build mew` files**, go.mod's mew
require, and `garland/` (upstream's own in-repo mirror, which we neither own nor
send).

### The v0.1.20 sync (record)

v0.1.19-alpha -> **v0.1.20-alpha**, one bug fix as PR
[#34](https://github.com/phroun/kittytk/pull/34): the provisional corral never
reached the GPU compositing path, so shrinking the SDL/WebGPU desktop lost
windows off the edge and growing it did not bring them back.

Two paths position a window and only one knew about the corral.
`WindowManager.Paint` (software renderer, TUI) asks `m.displayBounds`;
`renderer_webgpu.go` composites each window as a layer of its own and read
`win.Bounds()` through a locally-declared `WindowLike` interface — `grep
displayBounds sdl/` found nothing. The corral was written in `3e79714`
(2026-07-07) against the software paint loop, which was the only path then; the
WebGPU renderer arrived in `ec5494f` (2026-08-03) with the v0.1.7 overlay of
upstream's renderer migration. Nothing was deleted — a new path simply never
learned about it.

Two reasons it hid for a month. Every corral test drives `WindowManager` or
`MDIPane` directly, and nothing asked where a window landed on the compositor;
and `SoftwareRenderer.RenderFrameWithChildWindows` returns "child window
compositing not supported" and falls back to the paint loop, so only the WebGPU
path shows it — which is mew-sdl's default renderer (`hostconf.go` sets
`cfg.Renderer = "webgpu"`, where upstream defaults to software).

The tell: hit-testing DID go through the container, so a window off the edge
was clickable at the corralled position and inert where it appeared. The fix
makes the corral readable from outside the container — `Window.DisplayBounds`
asks a delegate that `WindowManager` and `MDIPane` both point at their own
`displayBounds`, so there is one corral rather than two implementations — and
the new test asserts the window's answer and the container's are EQUAL, since
that disagreement is the failure.

**The lesson for the next sync is about test shape, not code.** A behavior
implemented in one renderer and consumed by another needs an assertion at the
seam BETWEEN them. Green unit tests either side of a boundary say nothing about
the boundary.

Pin-only on our side: every shared file byte-identical, `Build` 19 -> **20**,
root and app go.mod pins v0.1.19-alpha -> v0.1.20-alpha, go.sum per module via
`GOWORK=off go mod tidy`. No dependency change.

### The v0.1.19 sync (record)

v0.1.17-alpha -> **v0.1.19-alpha**, landing the keymap-registry work as PR
[#32](https://github.com/phroun/kittytk/pull/32) plus the go.mod repair it
needed as [#33](https://github.com/phroun/kittytk/pull/33).

#32 is the KSP arc: key bindings become a `KeyRegistry` of keys to COMMANDS
with a `KeyContext` per situation (resolution through key-sequence-processor,
so chords and precedence levels come free); registries cascade down the
trinket tree so a guest that takes the keyboard leaves the toolkit's bindings
unresolvable while it has the focus; menu items name a command and ask what
key means it HERE, so the answer follows the focus; environment hints
(`(mac)`, `(only_mac)`) let one table describe every platform; and key names
are read off ONE table by parsing, macOS-native display and the screen reader
alike. It retired the parallel matcher (`Shortcut.Matches`, `ShortcutMap`,
`DefaultShortcuts`, `Action.MatchesKey`, `ActionGroup.HandleKey`,
`KeyBindings`' key lookups) and `Application`'s panicking event loop, which
was the tree's only `go vet` complaint. 109 files, +9513/-1816, cut from our
vendored tree.

**New dependency — key-sequence-processor v0.1.5** (MIT, stdlib-only, so not
a mew-only dep by the §2 licence test). Per fork-sync-policy §3 the PR sent
this as a sentence rather than a go.mod diff, and **the `go get` landed after
the tag instead of before it**: v0.1.18-alpha does not build at the default
`-mod=readonly`, because `core/keymap.go` imports a module `go.mod` never
required. #33 declares it (and `go mod tidy` promoted `golang.org/x/sys` from
indirect to direct, its own correction of a pre-existing classification).
**v0.1.18-alpha is permanently broken** — the module proxy caches immutably by
version, so re-tagging could not have repaired it and v0.1.19-alpha is the
fix. The verification lesson is worth keeping: check a candidate tag with
`GOWORK=off go build ./...` at the DEFAULT `-mod=readonly`, never with
`-mod=mod`, which resolves and writes the missing require on the fly and
hides exactly this failure.

Our side is otherwise a **pin-only** resync: every shared file is
byte-identical to the tag, so nothing came back down. `Build` 17 -> **19**,
kittytk's own go.mod keyseq v0.1.4 -> v0.1.5, root go.mod pin v0.1.17-alpha ->
v0.1.19-alpha, app/go.mod pin v0.1.16-alpha -> v0.1.19-alpha (it had lagged a
release), go.sum per module via `GOWORK=off go mod tidy`. The vendored tree
also carries direct-key-handler v0.3.17 and purfecterm v0.2.40 where upstream
sits at v0.3.12/v0.2.30 — those were NOT sent, and upstream builds and tests
green at its own versions, so no bump was needed. The only remaining
vendored<->upstream divergence is the mew boundary: the **25** `//go:build
mew` files (the set has grown; `capture_relay_test.go` joined it) plus
go.mod's mew require.

### The v0.1.7 sync (record)

v0.1.5-alpha -> **v0.1.7-alpha** was done as a full content overlay (upstream
had migrated the renderer to WebGPU across ~800 commits, and every shared
change our fork carried had already been upstreamed as PRs #15/#16/#17). The
overlay adopted v0.1.7 wholesale, restored the 19 fork-only files on top, and
kept go.mod's mew require. mew builds and tests green against it (plain, the
KittyTK TUI host, and the -tags mew trinket incl. editor_mew_*_test.go).
Because our shared changes were already upstream, there was nothing to
re-apply — only WebGPU + upstream's own work to adopt.

### The v0.1.16 sync (record)

v0.1.15-alpha -> **v0.1.16-alpha** is three graphical-host fixes plus the
SDL3-migration cleanup, all in one PR
([#30](https://github.com/phroun/kittytk/pull/30)): the solo maximize->restore
swapchain reconfigure (`SetScreenSizePx` clears an OS `MAXIMIZED` flag and
reconfigures from the window's real pixel size; adds `sdl3.WINDOW_MAXIMIZED` +
`Window.SizeInPixels()`), a divide-by-zero guard so compositing a child/overlay
onto a host dragged to ~0 area no longer panics `NewRGBA`, and dropping the
straggling SDL2 references (`buildwin.sh` rewritten to a purego `GOOS=windows go
build`, comments corrected). Cut from our vendored tree, so every shared file is
byte-identical to the tag; `Build` was already **16** (bumped ahead in that
work), so this was a **pin-only** resync: root `go.mod` + `app/go.mod` kittytk
pin v0.1.15-alpha -> v0.1.16-alpha, go.sum updated per module via `GOWORK=off go
mod tidy` (garland untouched). purfecterm stays v0.2.31. The only remaining
vendored<->upstream divergence is the mew boundary (the 20 `//go:build mew`
files + go.mod's mew require).

### The v0.1.15 sync (record)

v0.1.14-alpha -> **v0.1.15-alpha** is the grid cap and nothing else:
`v0.1.14-alpha..v0.1.15-alpha` = PR
[#29](https://github.com/phroun/kittytk/pull/29) (`clampGridDim`/`maxTermGridDim`
in `purfecterm.go` + `purfecterm_gfx.go` + `purfecterm_gridcap_test.go`) — the
degenerate-fit backstop the vendored tree had been carrying **ahead** of release
since the v0.1.14 record. It was cut from our tree, so those three files are now
byte-identical to the tag and the ahead-of-release divergence is retired. The
vendored `Build` was already **15** (bumped ahead in the v0.1.14 sync), so this
was a **pin-only** resync: root `go.mod` + `app/go.mod` kittytk pin
v0.1.14-alpha -> v0.1.15-alpha, go.sum updated per module via `GOWORK=off go mod
tidy` (garland untouched, no revert). purfecterm stays at v0.2.31. The only
remaining vendored↔upstream divergence is the mew boundary (the 20 `//go:build
mew` files + go.mod's mew require). The resize-runaway scaffolding that briefly
lived in `editor_mew.go` (the `MEW_FITLOG` trace and the fixed-`SizeHint`
feedback break) has since been removed: with the real fixes shipping —
purfecterm v0.2.31 (smart-wrap) + kittytk v0.1.14 (geometry-only fit) — the
`SizeHint` override proved redundant (the harness stays bounded without it) and
the trace had done its job. The grid cap stays as the degenerate-fit backstop.

### The v0.1.14 sync (record)

v0.1.13-alpha -> **v0.1.14-alpha** landing one fork PR, plus a purfecterm
dependency bump, plus one shared-file delta carried **ahead** of its upstream
release:

- [#28](https://github.com/phroun/kittytk/pull/28) (v0.1.14) — size the child
  terminal grid from geometry only. `paintGraphical` reserved a grid *row* for
  the horizontal scrollbar whenever `hScrollActive()` was true, but that
  predicate is content-dependent and the fitted grid is emitted to the child
  from inside `Paint` — so the child's row count fed back on its own output and
  self-sustained a `Resize -> redraw -> re-fit` loop at a fixed window size. The
  fit is now a pure function of geometry (columns from width minus the
  content-*independent* vertical lane, rows from full height); the horizontal
  bar overlays the bottom row instead of stealing one. `objects/trinkets/
  purfecterm.go` + `purfecterm_gfx.go` + `purfecterm_gridfit_test.go`. Cut from
  our vendored tree (`a1b85c6`, local-ahead of the release), so those files were
  byte-identical to the tag.

**Dependency bump — purfecterm v0.2.30 -> v0.2.31.** The nested host feeds the
inner mew's output into the outer PurfecTerm emulator; during a host resize the
inner content transiently exceeds the outer grid width and the outer autowraps
each animation frame. purfecterm's smart word wrap (mode 7702) *prepended* the
indent to the wrap-target row on every wrap, and because that target is a real
grid row an app has already painted, nothing truncated the surplus — an
unbounded line that drove memory and a quadratic visual-width scan until the
process was OOM-killed (the htop/`sl`-while-resizing freeze). Fixed upstream as
[purfecterm#20](https://github.com/phroun/purfecterm/pull/20) (released v0.2.31)
by overwriting the wrap target in place instead of prepending; the feature stays
enabled and the intended empty-target reflow case is unchanged. Pins bumped in
`kittytk/go.mod` and `app/go.mod`; see `patches/purfecterm/
smartwrap-overwrite-target.patch`. Validated end-to-end against the **released**
v0.2.31 (no workspace replace): the resize stress harness that reliably ran RSS
past 600 MB -> GBs now stays bounded and returns to baseline.

**Shared-file delta ahead of v0.1.15 — the grid cap.** The vendored tree also
carries a `clampGridDim`/`maxTermGridDim` (2000) backstop in both fit paths
(`purfecterm.go` `updateTerminalSize`, `purfecterm_gfx.go` `paintGraphical`) +
`purfecterm_gridcap_test.go`, guarding a *different* failure mode from #28's
churn: a degenerate fit (near-zero cell size, or a size-feedback runaway) sizing
the grid to hundreds of thousands of cells and OOM-allocating. It is **not** in
v0.1.14 — it is upstreamed as
[#29](https://github.com/phroun/kittytk/pull/29) and destined for v0.1.15, so
`purfecterm.go`/`purfecterm_gfx.go` diverge from the v0.1.14 tag by exactly the
cap. Following the `a1b85c6` precedent, `core/version.go`'s `Build` runs ahead
at **15** (the release that will contain the cap) while the `go.mod` pins track
the last actual release, v0.1.14-alpha. When v0.1.15 is cut, the follow-up
resync confirms byte-identity and bumps only the pins.

The fork boundary is otherwise unchanged (the **20 `//go:build mew` files** +
the mew require). At the time of this sync `editor_mew.go` additionally carried
temporary resize-runaway scaffolding — the `MEW_FITLOG` trace and the
fixed-`SizeHint` feedback break — both since removed once the release build was
re-confirmed (see the v0.1.15 record). This sync updated `go.sum` per module via
`GOWORK=off go mod tidy` (not `go work sync`), so the `garland` indirect was
**not** disturbed this time — no revert needed.

### The v0.1.13 sync (record)

v0.1.11-alpha -> **v0.1.13-alpha** in one step — the v0.1.12 pin bump was
folded in here rather than recorded on its own — landing three fork PRs and
nothing else. The combined diff `v0.1.11-alpha..v0.1.13-alpha` is exactly the
files those PRs touched:

- [#25](https://github.com/phroun/kittytk/pull/25) (v0.1.12) — WebGPU renderer
  leaked one native command encoder **per presented frame**: both present paths
  (`Present` and the desktop compositor `RenderFrameWithChildWindows`) ran
  `encoder.Finish()` → `queue.Submit()` but never `cmdBuffer.Release()`, which
  is what recycles the HAL encoder into the device pool. A live window resize
  forces a present per resize event, so rapid resizing inflated native memory
  without bound until the OS killed the process (`sdl/renderer_webgpu.go`).
- [#26](https://github.com/phroun/kittytk/pull/26) (v0.1.13) — `PurfecTerm`
  read-only mirror paint: `PaintMirror` renders one live terminal in several
  places, the extras drawn UNFOCUSED (hollow cursor, no platform caret) and
  sizing/scrolling nothing (`mirrorPaint` gates `updateTerminalSize`, the gfx
  fit-resize, and the `CheckCursorAutoScroll`/`ClearDirty` side effects;
  `paintFocused()` is the new render-focus predicate). `objects/trinkets/
  purfecterm.go` + `purfecterm_gfx.go` + `purfecterm_embeddedfocus_test.go`.
- [#27](https://github.com/phroun/kittytk/pull/27) (v0.1.13) — the
  `profile://`/`box://` scheme-architecture design doc, plus the `sdl3.go`
  comment mojibake fix (see below). Docs/comment only, so **no build-counter
  bump** (docs-only changes don't move `Build`).

Cut from our vendored tree, so every changed file was byte-identical to the
release; the sync bumped `core/version.go`'s `Build` to 13 and the `go.mod`
pins to v0.1.13-alpha. No dependency bumps; no shared-interface change. The
fork boundary is unchanged (**20 `//go:build mew` files** + the mew require).
`sdl/sdl3/sdl3.go`'s comment em-dashes now match upstream both ways — #27
fixed the mojibake upstream, so the long-standing divergence is retired.

`go work sync` again wrongly bumped the vendored `kittytk/go.mod`'s `garland`
indirect (`v0.1.8 -> v0.1.11`); reverted, as in the v0.1.10 record — upstream
carries no `garland` require, and the indirect is a mew-boundary artifact a
kittytk resync must not touch.

### The v0.1.11 sync (record)

v0.1.10-alpha -> **v0.1.11-alpha** is one fork PR landing upstream and nothing
else — the diff `v0.1.10-alpha..v0.1.11-alpha` is exactly the three files it
touched:

- [#24](https://github.com/phroun/kittytk/pull/24) — TUI backend asserts a
  single-width DEC line baseline on startup: `Init` arms the per-line `DECSWL`
  (`ESC#5`) sweep so the first present clears any stale `DECDWL` a previous
  session left in the alternate screen (which survives the `?1049h` switch and
  an erase — only `DECSWL`/`RIS`/soft-reset retire it). A per-line sweep is used
  rather than `DECSTR`/`RIS`, which would also tear down the alt-screen, mouse,
  and keyboard state `Init` just set up (`backend/tui/tui.go` + test).

Cut from our vendored tree, so every changed file was byte-identical to the
release; the sync bumped `core/version.go`'s `Build` to 11 and the `go.mod`
pins to v0.1.11-alpha. No dependency bumps; no shared-interface change. The
fork boundary is unchanged (**20 `//go:build mew` files** + the mew require).
`sdl/sdl3/sdl3.go`'s comment em-dashes stay correct on our side (upstream still
carries the mojibake — a trivial upstream fix for later).

See `patches/kittytk/tui-startup-dwl-baseline.patch`.

### The v0.1.10 sync (record)

v0.1.9-alpha -> **v0.1.10-alpha** is one fork PR landing upstream and nothing
else — the diff `v0.1.9-alpha..v0.1.10-alpha` is exactly the files it touched:

- [#23](https://github.com/phroun/kittytk/pull/23) — deliver a bracketed paste
  as a whole `PasteEvent` instead of a dropped key-flood (new `core.PasteHandler`
  routed through the focus/window/mdipane chain; `PurfecTerm`/`TextInput`
  implement it), plus a `Button.animatingPress` data-race fix, plus the
  fork-sync-policy build-counter rule.

Cut from our vendored tree, so every changed file was byte-identical to the
release; the sync bumped `core/version.go`'s `Build` to 10 and the `go.mod`
pins to v0.1.10-alpha. Dependency bump (shared, never mew): `direct-key-handler`
`v0.3.11 -> v0.3.12` (the `OnPaste` callback + `EmitPasteKeys` option the paste
fix needs). The fork boundary grew by one to **20 `//go:build mew` files**
(`editor_mew_paste_test.go` joins) + the mew require.

Two things a naive `go work sync` gets wrong here, both handled:

- It bumped the vendored `kittytk/go.mod`'s `garland` indirect (`v0.1.8 ->
  v0.1.11`). Reverted: upstream has no garland require at all — the `garland/`
  dir is just a co-development mirror nothing in kittytk imports. The indirect
  is a mew-boundary artifact (kittytk trinkets -> mew -> mew/internal/buffer ->
  garland), not a kittytk dep, so a kittytk resync must not touch it.
- Upstream's `sdl/sdl3/sdl3.go` carries mojibake em-dashes (`â`) in comments
  where our vendored copy has correct `—`. Left ours correct rather than
  regressing to the corruption; it is comment-only and does not affect the
  build. (Worth a trivial upstream fix later.)

See `patches/kittytk/{bracketed-paste,button-animatingpress-race}.patch`.

### The v0.1.9 sync (record)

v0.1.8-alpha -> **v0.1.9-alpha** is three more fork fixes landing upstream and
nothing else — the diff `v0.1.8-alpha..v0.1.9-alpha` is exactly the files those
PRs touched (the other merges in the range, #22/#13, netted no file change):

- [#20](https://github.com/phroun/kittytk/pull/20) — TUI backend: pixel-precise
  mouse from the outer terminal (`?1016`) (`backend/tui/tui.go` + test).
- [#21](https://github.com/phroun/kittytk/pull/21) — solo tear-off double-host
  re-entrancy guard, and the WebGPU rotation easter egg gated to the About
  KittyTK dialog (`objects/trinkets/desktop*.go` + `sdl/platform_sdl.go` +
  tests).

All three had already been applied to our vendored tree (that is where the PRs
were cut from), so every changed file was byte-identical to the release; the
sync only bumped `core/version.go`'s `Build` to 9 and the `go.mod` pins to
v0.1.9-alpha. The fork boundary is unchanged (the 19 `//go:build mew` files +
the mew require). See `patches/kittytk/{tui-outer-pixel-mouse,
solo-tearoff-reentrancy,about-rotation-gate}.patch`.

### The v0.1.8 sync (record)

v0.1.7-alpha -> **v0.1.8-alpha** is the SetHint pre-load fix and nothing else:
`v0.1.8-alpha` = `v0.1.7-alpha` + PR
[#19](https://github.com/phroun/kittytk/pull/19) (`sdl/sdl3/sdl3.go` loads
libSDL3 before a pre-Init `SetHint`, so a host that sets an OS app name — as
mew-sdl does — no longer segfaults on launch; build counter 7 -> 8). The fix
had already been applied to our vendored tree to unblock mew-sdl, so its
`sdl3.go` was byte-identical to the release; the sync was just bumping
`core/version.go`'s `Build` to 8 and the `go.mod` pins to v0.1.8-alpha. The
fork boundary is unchanged (still the 19 `//go:build mew` files + the mew
require). See `patches/kittytk/sdl-sethint-preload.patch`.

## Cover note for a PR

Upstream asks each sync to state: the tag diffed against, dependency bumps in
words (never a `go.mod` diff), and any change to a shared interface, so they
can sweep for unimplemented test doubles.
