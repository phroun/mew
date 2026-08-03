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

### The v0.1.7 sync (record)

v0.1.5-alpha -> **v0.1.7-alpha** was done as a full content overlay (upstream
had migrated the renderer to WebGPU across ~800 commits, and every shared
change our fork carried had already been upstreamed as PRs #15/#16/#17). The
overlay adopted v0.1.7 wholesale, restored the 19 fork-only files on top, and
kept go.mod's mew require. mew builds and tests green against it (plain, the
KittyTK TUI host, and the -tags mew trinket incl. editor_mew_*_test.go).
Because our shared changes were already upstream, there was nothing to
re-apply — only WebGPU + upstream's own work to adopt.

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
