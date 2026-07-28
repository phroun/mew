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

    objects/trinkets/editor_mew.go
    objects/trinkets/editor_mew_editactions_test.go
    objects/trinkets/editor_protocol_mew.go
    go.mod / go.sum      (the github.com/phroun/mew require and its deps)

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

A `subtree split` today differs from upstream by exactly the five fork-only
files above: **+861 lines, zero deletions**. The deletions cannot be proposed
because upstream's content simply sits where upstream put it.

## Cover note for a PR

Upstream asks each sync to state: the tag diffed against, dependency bumps in
words (never a `go.mod` diff), and any change to a shared interface, so they
can sweep for unimplemented test doubles.
