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
*Researched 2026-08-24. The title used to cover four properties and an event;
three properties and the event are done, and this is what is left.*

`docs/d2-read-audit.md:103-108` records the caret as C2 exception #3:
*"`change` carries `text=` only. Fine for v1 (most apps only need text); if an
app needs caret tracking, add `cursor=` to `change` or a `caret` event.
Deferred, recorded in the vocabulary doc."*

So this is a decision with an open follow-up rather than an oversight, and the
two options it names are still the question. Reversing it needs the event half
designed, not just properties registered — a `cursor` property a client can
write but never read tells it nothing about where the caret went.

`docs/property-vocabulary.md:196-197` now carries `cursor`, `selection_start`
and `selection_end` marked **not implemented** against this deferral, so the
docs and the wire agree on their absence.

Go API a wire client still cannot reach: `SetCursorPosition`, `SelectAll`,
`SelectedText`, `HasSelection`.
*Verified against current code and docs.*

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

### Panel: give flex and grid a wire spelling
**Title only.** Related surface: `objects/trinkets/panel_protocol.go:68` reaches
the layout manager through an interface assertion for `SetSpacing`, so layout
properties are currently spelled ad hoc.

### MenuItem: decide what an item with no `action=` should emit
**Title only.** A design question, not a bug report — the entry records that the
behaviour is undecided, not that it is wrong.

### item: no separator, and `ListItem.Enabled` is unreachable from the wire
Two things in one entry, both about list items: there is no way to spell a
separator, and `Enabled` is implemented but has no wire property.
**Title only beyond that** — the "implemented" half is unverified.

### Window: `type` reads as an enum but is registered as a string
**Title only.** Presumably accepts any string and silently ignores unknown values.

### Menu: registered non-virtual, but is not a trinket
So common trinket properties fail against it with an internal error rather than a
useful message. **Title only** beyond that.

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

### TreeView: duplicate column id silently shares one value slot
Two columns declared with the same id write to the same place, with no complaint.
**Title only** beyond that.

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

### Separate the client shims from the toolkit
Split the protocol by role, lift the in-process transport out, and give the Go
shim its own module. The largest item on this list and the one most likely to
have gone stale. **Title only** beyond that summary.

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

---

## Not on this list

These are **done** and need no issue.

From the task list this file was written from: the space bar command
(`f0882e7`), the `regTrinket` events-param sync landmine, `-tags mew` building
a host with no editor, retiring listview's `alternate_rows` in favour of
`ledger` (`55c3114`), and validating `sub`/`unsub` event names (`40ee186`). The
SDL space-bar naming bug found while writing this is fixed in `78ed16a`.

Since then, out of the TextInput entry above:

- `readonly`, `max_length`, `echo` (`normal`/`password`/`none`) and `mask` are
  registered (`ea6de01`). The vocabulary doc had `mask` as "flag or string"
  with an explicit character while the paint path hardcoded a bullet, so the
  character half could not have worked — a `maskChar` was added for it, and
  turning masking on became `echo`, which also reaches "paint nothing", a mode
  the flag spelling could not express.
- A completion event exists (`96279c4`). `SetOnReturnPressed` was in-process
  only and Bind wired just `SetOnTextChanged`, so `change` was the sole event
  textinput declared and a wire client could watch every keystroke without ever
  learning the person was finished. The callback is now `SetOnComplete` and
  raises `complete`, carrying `trinket` and `text`.

And found by live testing rather than from this list: a disabled trinket taking
focus (`b39ce28`), a disabled field editable through its own context menu
(`5408dea`), and the read-only caret, which is now a block that inverts
whatever it sits on (`e540266`, `e1bb9c9`), over a disabled field painted in
`DisabledTextFG` on its container's ground (`de42973`).
