# Sync delivery: menu anchors

Cover note for `0001-kittytk-menu-anchor.patch`, prepared per
`kittytk/docs/fork-sync-policy.md`.

## Provenance

| | |
|---|---|
| Upstream tag diffed against | `v0.1.5-alpha` (`133d697`) |
| How it was produced | `git subtree split --prefix=kittytk`, then the four `objects/trinkets/` files replayed onto a clean `v0.1.5-alpha` worktree — **not** a recursive `diff` of our tree |
| Files touched | `objects/trinkets/{desktop.go, menu.go, menu_protocol.go}`, new `objects/trinkets/menuanchor_test.go` |
| Size | +202 / −1 |

## Dependency bumps

**None.** No `go.mod` or `go.sum` edit is in this patch; nothing new is
imported.

## Shared-interface changes

**None** — this is the item the policy asks us to call out, and the answer is
that there is nothing to sweep for.

`Menu` gains two methods (`SetAnchor`, `Anchor`) and one unexported field.
`Menu` is a concrete struct, not an interface, so no test double has to grow a
stub. No existing signature changes. `menuBuckets` gains a field and a helper,
both package-private. A menu with no anchor takes exactly the path it took
before.

## What it does

A well-known tag was doing two jobs at once: declaring a menu's **role**
(inject the clipboard items into edit, the window list into window) and fixing
its **position** in the canonical sequence. Untagged menus have no role, so
they got no position either — they collected in one block after view. An app
whose menu order is deliberate had no way to express it except by dropping its
well-known tags, which forfeits the role behaviour along with the placement.

`Menu.SetAnchor` (protocol attribute `after="file"`) separates the two:

```
new menu caption="Search" after="file" children={ ... }
```

- An untagged menu may sit immediately after a well-known **slot**.
- Menus sharing an anchor keep their declared order.
- Unanchored menus keep the trailing custom block, unchanged.
- An anchor on a menu that *is* tagged is ignored — its role fixes its place,
  so a standard menu never moves silently.

The anchor names the **slot**, not a live menu. Anchoring after `file` in an
app that declares no file menu still lands ahead of edit, so a layout does not
shift when a neighbouring menu is added or removed.

Deliberately **not** expressible: "put this third". An app can only say "after
the file menu", so the canonical roles remain the frame of reference even for a
bar that departs from the default order. The standard guides rather than
dictates — which seemed the right reading of what the well-known IDs are for.

## The motivating case

mew's menu bar is a weaning system for the WordStar key families rather than a
platform-standard set, so it wants Input and Search beside its file menu and
History beside Format:

```
File Buffer   Input   Search   Edit Block   Format   History   Viewport   Window   Help
```

Three anchors express that, and every well-known menu stays exactly where its
role puts it. The patch's fourth test asserts that bar end to end (named
generically — the fork-specific caption set stays on our side).

## Verification on a pristine upstream tree

Applied to a clean `v0.1.5-alpha` checkout with **no mew module in the graph**:

```
GOWORK=off go build ./...          # clean
GOWORK=off go test ./...           # all packages pass
gofmt -l objects/trinkets/         # clean
```

`go vet ./objects/...` reports one pre-existing `unreachable code` at
`objects/app/application.go:450`, untouched by this patch.

## Self-audit (the five greps from §3 of the policy)

| Check | Result |
|---|---|
| 1. Anything under `garland/**` or `patches/**` | clean |
| 2. `phroun/mew` anywhere in the diff | clean |
| 3. README funding/licence lines removed | clean |
| 4. `Binary files` blocks | clean |
| 5. Removals dwarfing additions | +206 / −5 raw lines — feature-forward |
| (extra) `core/version.go`, `go.mod`, `go.sum`, `README.md` touched | clean |
