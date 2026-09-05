# Open findings — unassessed

These were collected across working sessions in a scratch task list that does not
survive the session. They are written down here so they outlast it.

**Every one of these is a candidate, not a verdict.** They have not been triaged,
prioritised, or in most cases re-checked against current code. Some were recorded
in earlier sessions and only the one-line statement survives — those are marked
**title only**, and the body is what the title says plus nothing. Where I had a
real anchor or a live detail, it is included and marked as such.

One entry (**Submenus, in practice**) is known to be wrong and is kept only so the
wrong version does not get filed by accident.

Repo attribution is my reading, not a decision — most of this is toolkit work
sitting in mew's vendored `kittytk/`, and would go upstream.

---

## KittyTK — protocol / wire surface

### TextInput: `cursor` and `selection` on the wire
*Researched 2026-08-24.*

`docs/d2-read-audit.md:103-108` records the caret as C2 exception #3:
*"`change` carries `text=` only. Fine for v1 (most apps only need text); if an
app needs caret tracking, add `cursor=` to `change` or a `caret` event.
Deferred, recorded in the vocabulary doc."*

So this is a decision with an open follow-up rather than an oversight, and the
two options it names are still the question. Reversing it needs the event half
designed, not just properties registered — a `cursor` property a client can
write but never read tells it nothing about where the caret went.

Go API a wire client still cannot reach: `SetCursorPosition`, `SelectAll`,
`SelectedText`, `HasSelection`.
*Verified against current code and docs.*

### `parent=` has no wire spelling: containment is children-blocks only
*Recovered 2026-09-05 from the retired `docs/property-vocabulary.md` draft.*

The draft's identity table listed `parent=<id or key>` as how an object states
its containment "at creation or reparent". It was never registered: it is not a
common property, and nothing in `protocol/` resolves it. Structure is built with
`children={}` blocks and nothing else.

Two consequences, and the second is the sharper one:

- A trinket can only be placed where it is created. There is no wire spelling
  for **reparenting** at all -- `destroy` and rebuild is the only route.
- A build script's shape is forced to match the tree's shape. Anything that
  wants to declare objects flat and then assemble them cannot.

Whether `parent=` should exist is the open question; if it does, it needs an
answer for what happens when it names an object in another connection's tree,
and for whether it can move a trinket that is already placed.

### Forward references within a batch
*Recovered 2026-09-05 from the retired `docs/property-vocabulary.md` draft,
where it was open question 5, marked "left open by owner".*

Whether a later statement in one batch may reference a correlation key bound by
an earlier one:

```
key1=new window ...
new button parent=key1 ...
```

The draft's argument for it was building whole trees in one burst. It depends on
`parent=` above, so the two stand or fall together, and it is the reason to
decide `parent=` rather than simply drop it.

Against: `children={}` already builds a whole tree in one burst, and scoped keys
(`k1.sk1`) already address inside one. What forward references add is the flat
declaration order, not the single round trip.

### TextInput: `EchoPasswordOnEdit` is declared but does nothing
`EchoPasswordOnEdit` is one of four `EchoMode` constants (`textinput.go:101`,
documented "Show char briefly, then bullet"), but `echo()` switches only on
`EchoPassword` and `EchoNoEcho` — the constant falls through to `default` and
paints the text in the clear. A field set to it is not masked at all.

Found while registering the echo modes on the wire. It is deliberately NOT one
of the `echo` enum's words, because exposing it would put a spelling on the
wire that silently does the opposite of what it says. Either implement it or
delete the constant.
*Verified against current code.*

### MenuItem: decide what an item with no `action=` should emit
**Title only.** A design question, not a bug report — the entry records that the
behaviour is undecided, not that it is wrong.

### item: no separator, and `ListItem.Enabled` is unreachable from the wire
Two things in one entry, both about list items: there is no way to spell a
separator, and `Enabled` is implemented but has no wire property.
**Title only beyond that** — the "implemented" half is unverified.

### Window: `type` reads as an enum but is registered as a string
**Title only.** Presumably accepts any string and silently ignores unknown values.

### DockEntry: `window=` is captured at append time
Retargeting an entry afterwards is silently ignored.
Anchor: `objects/trinkets/dock_protocol.go:53` is where the `window` property is
bound; `objects/trinkets/desktop.go:831` and `dock_protocol.go:89` construct the
`DockEntry`. *Anchors verified; the capture-at-append claim is not re-verified.*

---

## KittyTK — behaviour

### TreeView: expand-all raises no expand events
A caller watching for expansion sees nothing when the whole tree opens at once.
Anchor: `objects/trinkets/treeview.go:479` (`ExpandAll`), called from
`treeview.go:1119`. *Anchor verified; event behaviour not re-verified.*

### MessageBox: Escape is swallowed but unanswered
When there is no cancel button, or no buttons at all, Escape is consumed and the
box neither closes nor answers. **Title only** beyond that.

### Session keys survive their object's parent being destroyed
The key table grows without bound. **Title only** beyond that — worth checking
whether it is a genuine leak or bounded in practice.

### `display.go` claims a Psi-menu lockdown toggle that does not exist
Recorded as a documentation/reality mismatch. **Caution:** `display/display.go:78`,
`:87` and `:91` do implement a lockdown (`SetPreTrustedOnly` / `PreTrustedOnly`),
so either the entry means a *different* toggle, or it means the comment describes
it as reachable from the Psi menu when it is not. Needs re-derivation before it is
worth filing.

---

## KittyTK — layout and the cell grid

### GridLayout: a spanning child contributes to no column's width
*Found 2026-09-05 while testing the column boundaries. Verified against current
code.*

`calculateColumnWidths` and the two size passes measure only children whose
`ColumnSpan` is 1 (`layout/grid.go`, the `item.ColumnSpan == 1` guards). A child
that spans several columns is therefore invisible to all of them, so columns
holding nothing else collapse to zero and the span comes out at the width of the
boundaries it crosses.

Reproduced: a 120-wide child spanning columns 0 and 1, with nothing else in
either column, in a grid 400 wide -- the span was laid out 8 units wide.

Qt divides a spanning item's width across the columns it covers, raising each
only as far as it must. The same is true of `RowSpan` and row heights. Either
distribute it, or say plainly that a span never sizes a column and leave the
author to give the columns minimums.

### Nothing keeps a trinket's bounds on the cell grid
On a cell surface, drawing rounds and hit-testing does not: `UnitsToCellX`
integer-divides (`backend/tui/tui.go:993`) while `UnitRect.Contains` works in
units. So a trinket placed off the grid draws in one cell and answers the mouse
in another.

This was hit for real: I set a window to `area.Width*3/4`, which has no relation
to `CellWidth`, and got sizes like 640×400 → 480×276 (`h % 16 == 4`). Fixed at
that one call site with `metrics.AlignSize`. Nothing enforces it generally.

The gate for whether snapping applies is `WindowManager.SmoothPositioning()`, set
only when the backend reports `core.SmoothPositioner`. The open question is
whether alignment belongs in `SetBounds` for every trinket under a cell surface,
rather than at each call site.
*Live detail from a session where it was reproduced.*

### BoxLayout: `SizeHint` counts spacing raw while `Layout` rounds it
The measurement and the placement disagree about the same gap, so a box asks for
one size and lays out at another. Anchors: `layout/layout.go:75-76`
(`BaseLayout.SetSpacing`), `core/trinket.go:198-199` (the interface).
*Anchor verified; the raw-vs-rounded claim is from an earlier reading and not
re-verified.*

---

## KittyTK — menus

### Submenus have no right-edge handling
A deep submenu chain walks off the right of the screen with nothing to flip or
clamp it. Observed behaviour.

### Submenus, in practice — **DO NOT FILE AS WRITTEN**
Submenus are unreliable on screen. They work over the wire to arbitrary depth,
with accelerators, separators and dispatch — that part was tested.

The version of this entry in the task list carried **my** explanation of why, which
you told me was wrong: *"your characterization of the submenus is completely wrong,
they need reworking for reasons you don't know yet."* I have not been told the real
reason, so there is nothing here to file. Kept only so the wrong text is not filed
by mistake.

---

## KittyTK — structure

### Give the client shim its own module
*Researched 2026-08-24. The entry used to cover three things; two are done
and this is the third, which is a decision rather than a refactor.*

`client` now imports `wire` and the standard library, nothing else — a
client-only build compiles two kittytk packages and no third-party code, and
`00_client_imports_only_the_wire_test.go` holds it there. So the remaining
question is only whether the shim gets its own `go.mod`.

What it would buy: an application writing a KittyTK client stops carrying
SDL3, webgpu, purego, purfecterm, garland and pawscript in its module graph,
and stops inheriting their version constraints. On this fork it also stops
carrying `github.com/phroun/mew` — though that half is fork-only, since the
sync policy keeps mew out of upstream's `go.mod` entirely.

What it would cost: it is two modules, not one, because `client` needs
`wire`. Nested modules keep their import paths, so `wire/go.mod` (a leaf with
no requires) and `client/go.mod` (requiring wire) work as they stand, with
`replace` directives for local development and subdirectory tags
(`wire/v0.1.x`, `client/v0.1.x`) for release. That is two more release
cadences to run and three `go.mod` files for the subtree split to carry.

Worth doing when there is an external client asking for it. Until then it is
release overhead buying nothing measurable, and it stays one file away.

### Wiki: add the trinkets missing from the Home index
Documentation gap. **Title only** — the list of which trinkets is not recorded.

### Fix the stale `SizeHint` comment on TextInput
A comment that no longer describes the code. Small. **Title only.**

---

## mew

### Scope the editor's dirty event to the launch document
Currently it fires for the whole session rather than for the document the editor
was launched on. **Title only** beyond that.

### Editor placeholder: make filename win, resolved through a host-brokered virtual FS
A design note rather than a defect. **Title only.**
Repo is ambiguous — the editor placeholder lives in the toolkit
(`objects/trinkets/editor_mew*`) but the behaviour is mew's.
