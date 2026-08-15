# Keeping the mew fork of KittyTK in sync with upstream

*A note to whoever maintains mew's vendored copy of `kittytk/` — human or AI.*

Thanks for the sync deliveries. The work in them is good. This doc exists
because **the way the diffs were generated made them dangerous to apply**, and
each one had to be hand-reconciled before it could land. If you follow the
policy below, your next diff will apply cleanly and I won't have to
second-guess it.

The core problem: a plain recursive `diff` of your vendored tree against
upstream describes *"make upstream look exactly like my working copy."* But
your working copy is a **fork with a different boundary** than upstream — it
drops some things, vendors others, and carries local build junk. A verbatim
diff therefore tells us to delete files we own, add dependencies we forbid,
and revert our own content. "Applies cleanly" (no merge conflicts) is **not**
the same as "is safe to apply."

---

## 1. The fork boundary — what is upstream's, what is yours

Upstream (`phroun/kittytk`) is the source of truth for the toolkit. Your fork
diverges in exactly these ways. A sync must respect all of them.

### Files that are UPSTREAM-OWNED — never send changes to these

| Path | Why it's ours, not yours |
|---|---|
| `README.md` header (MIT badge, ko-fi links, funding lines) | Project identity + funding. Your vendored copy strips these for its own reasons; that choice must **not** propagate back. This has been reverted **twice** now. |
| `garland/**` | Upstream vendors garland *in-repo* (a development mirror synced to garland releases). You consume garland as an **external module** and have no `garland/` dir — so a recursive diff reads its absence as "delete all 84 files." It is not a deletion; it's a boundary difference. |
| `patches/**` | Upstream's own patch archive. Same story: absent in your tree, so a diff wants to delete it. |
| `go.mod` / `go.sum` — the `github.com/phroun/mew` require and its mew-only transitive deps (`argwild`, `pawscript`, `garland` as a *direct* consumer, `uax29`, `go-runewidth`) | **mew must never appear in upstream's module graph.** mew's licence is more restrictive than the KittyTK base, so upstream is deliberately mew-free. Your `go.mod` naturally requires mew; that line is poison here. |
| `core/version.go` — `const Version` (major.minor) | Upstream hand-sets the release number. Don't change it in a sync. `const Build` is the exception: it **does** move with an upstream improvement — see §2a. |

### Files that ARE yours — keep them on your side, never send them

- `objects/trinkets/editor_mew.go`
- `objects/trinkets/editor_mew_editactions_test.go`
- `objects/trinkets/editor_protocol_mew.go`

These carry `//go:build mew` and import `github.com/phroun/mew`. Upstream ships
the complementary `//go:build !mew` placeholders (`editor.go`,
`editor_protocol.go`) instead. You already exclude these correctly with
`--exclude` — keep doing that. **Changes to the `!mew` placeholders are fine
to send** (they import nothing from mew and keep the two sides of the contract
in step).

### Junk that must never be in a diff at all

- Built binaries: `kittytk-sdl`, `kittytk-tui`, `demo`, `demoapp`, etc.
- `python/**/__pycache__/*.pyc`
- Anything matched by upstream's `.gitignore`.

Your recursive diff swept all of these in as "Binary files differ" blocks that
`git apply` can't even process. Clean your working tree first (see below).

---

## 2. The one legitimate thing that DID need to cross

Not everything in the module files is off-limits. The real dependency bumps
your code needs are welcome — e.g. `direct-key-handler v0.3.7 → v0.3.9` in the
last delivery was correct and I kept it. The rule is narrow: **bump shared
third-party deps freely; never introduce `mew` or mew-only deps.** If you're
unsure whether a dep is "mew-only," ask — don't guess.

---

## 2a. Always bump the build counter on an upstream improvement

Every time we send a real change upstream (a PR to `phroun/kittytk`, not a
pass-through resync), **bump `const Build` in `core/version.go` as part of that
change.** The build counter is not a private mew number to be stripped — it is
the third component of the KittyTK version, and it **always matches the third
number of the release tag**:

| Tag | `const Build` |
|---|---|
| `v0.1.9-alpha` | `9` |
| `v0.1.10-alpha` | `10` |
| `v0.1.11-alpha` | `11` |

So an upstream PR that will land as `v0.1.10` sets `const Build = 10`. Bump it
by one from upstream's current value (or run `make increment` on the upstream
checkout, which does the same thing) — never carry mew's own local counter
across; derive it from the tag the release will get. `const Version`
(`0.1`) is still upstream's to hand-set for a major/minor bump; leave it alone.

This is the one deliberate write to `core/version.go` that crosses the boundary
— the §1 table forbids everything else in that file (the `const Version` line),
not this.

---

## 3. How to generate a sync diff that is safe to apply

Until we set up shared history (section 4, the real fix), produce the diff
like this. It mechanically enforces every boundary above.

```sh
# From your fork root. UP = a clean checkout of the upstream tag you diverged
# from (e.g. v0.1.3-alpha). MINE = your vendored kittytk tree.

git -C "$MINE" clean -xdn          # DRY RUN: show build junk. Then -xdf to remove,
                                   # or just make sure it's all gitignored.

diff -ruN \
  --exclude='.git' \
  --exclude='editor_mew.go' \
  --exclude='editor_mew_editactions_test.go' \
  --exclude='editor_protocol_mew.go' \
  --exclude='garland' \
  --exclude='patches' \
  --exclude='__pycache__' \
  --exclude='*.pyc' \
  --exclude='kittytk-sdl' \
  --exclude='kittytk-tui' \
  --exclude='README.md' \
  "$UP" "$MINE" > sync.diff
```

Then, **before you send it**, self-audit — these are the checks I have to run
on the receiving end, so run them yourself:

```sh
# 1. Nothing under garland/ or patches/ (boundary leak):
grep -E '^\+\+\+ .*/(garland|patches)/' sync.diff && echo "LEAK — fix excludes"

# 2. No mew anywhere:
grep -i 'phroun/mew' sync.diff && echo "mew LEAK — strip go.mod/go.sum edits"

# 3. No README funding/licence lines being removed:
grep -nE '^-.*(ko-fi|License: MIT|ko-fi.com)' sync.diff && echo "README LEAK"

# 4. No binary blobs:
grep -E '^Binary files ' sync.diff && echo "JUNK — clean the tree"

# 5. Sanity: removals shouldn't dwarf additions for a feature-forward sync.
#    A huge negative line count means you're deleting things you don't own.
```

For `go.mod`/`go.sum`, **do not diff the files**. Instead, send a plain-text
note: *"bump `direct-key-handler` to `v0.3.9`"* and let upstream run
`go get` + `go mod tidy` itself. `tidy` on the mew-free tree will never pull
mew back in, because no upstream source imports it.

Include a short cover note listing: the upstream tag you diffed against, the
dep bumps (in words), and any new interface method you added (see next point).

### If you add a method to a shared interface, say so

The last delivery added `SetCursorStyle(int)` to `core.RenderBackend`. That
broke two upstream test doubles (`fakeBackend`, `nullBackend`) that your tree
doesn't have. It's not your job to know upstream's private test fakes — but
**do call out interface changes in the cover note** so the receiver knows to
sweep for unimplemented stubs.

---

## 4. The real fix: stop generating "diff against a snapshot"

The steps above are damage control. The underlying issue is that your
vendored `kittytk/` "shares no git history with upstream (it was imported as
files)," so every transfer is a blind snapshot diff. Two ways to fix that
properly, best first:

### Option A — `git subtree` (recommended)

Pull upstream into your monorepo as a subtree, and let git track the merge
base for you. Then syncs are real merges/splits, not hand-rolled diffs.

```sh
# One-time, in your monorepo:
git remote add kittytk-upstream https://github.com/phroun/kittytk
git subtree add --prefix=kittytk kittytk-upstream v0.1.3-alpha --squash

# Pull upstream updates later:
git subtree pull --prefix=kittytk kittytk-upstream <newtag> --squash

# Send YOUR changes back as a clean branch upstream can PR-review:
git subtree split --prefix=kittytk -b kittytk-sync
#   ...push kittytk-sync to a fork of phroun/kittytk and open a PR.
```

With this, the boundary is enforced by *what lives under `kittytk/`* — garland
and patches simply aren't in the subtree, so they can never appear as
deletions. mew-tagged files live **outside** the subtree prefix (or are
`.gitignore`d within it) and never enter a split.

### Option B — submodule

Make `kittytk/` a real git submodule pointing at `phroun/kittytk`, and keep
**all** mew-specific code (the `//go:build mew` editor, mew's `go.mod`
requires) in your outer repo, *never* inside the submodule working tree. Your
mew module imports the submodule as a Go dependency. Upstream stays pristine
by construction; you literally cannot commit a mew require into it.

This is cleaner for the licence boundary (mew code and upstream code never
share a working tree) but heavier day-to-day (detached-HEAD submodule dance).
Subtree is usually the better fit for an actively co-developed pair.

### Either way — the invariant

> Upstream's tree must be buildable and testable **with no mew module anywhere
> in the graph**: `GOWORK=off go build ./... && go test ./...` passes with a
> clean `go.mod` that does not mention `github.com/phroun/mew`.

If a change you want to send can't satisfy that, it belongs on your side of the
boundary, not upstream's.

---

## TL;DR

1. Never send changes to `README.md` funding/licence, `garland/**`,
   `patches/**`, `core/version.go`'s `const Version`, or anything
   importing/vendoring `mew`.
2. **Do** bump `core/version.go`'s `const Build` on every upstream improvement,
   to match the third number of the release tag (§2a).
3. Exclude build artifacts and `__pycache__` from the diff.
4. For deps, send a *sentence* ("bump X to vN"), not a `go.mod` diff — and
   never a mew require.
5. Run the five self-audit `grep`s before sending.
6. Long-term: adopt `git subtree` so the boundary is structural, not a set of
   `--exclude` flags you have to remember every time.
