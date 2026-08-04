# Viewport tiler — interactive model demo

`index.html` is a single self-contained page (no build, no dependencies — open it
in a browser) that models the **viewport tiling system** we intend to extract as
a standalone, host-agnostic Go module that mew depends on and KittyTK can reuse.
It exists to validate the algorithms and ergonomics *before* they are written in
Go. Everything runs client-side; there is no server component.

## What it exercises

- **Tree of oriented groups** — leaves inside groups with four orientations
  (LTR / RTL / TTB / BTT = two axes × two polarities). Reading order is child
  index order.
- **Normalization** — same-orientation flatten, single-child collapse, and
  dismiss cascade to a canonical form.
- **Two-pass constraint-negotiation layout** — bottom-up `measure`
  (min/natural bounding box) then a top-down `allocate` waterfall: minimums by
  priority → pins → zoom → normal-toward-natural → mop-up, with cross-axis
  omission reported the same way everywhere.
- **Modes & pins** — zoom / shrink / normal, manual drag-pins, and relative
  grow/shrink (`−` / `=`) that pins `resolved ± stride` off the last layout so a
  host never precomputes a size.
- **Structural directional nav** — walk up to the nearest ancestor whose axis
  matches travel, then descend under the caret's goal coordinate.
- **Verbs** — new / split (cross vs absolute) / swap (caret-preserving) /
  merge / flip / reverse / dismiss / tile-cycle.
- **Merge band invariant** — a merge never adds a band in the direction of
  travel: the source lands adjacent to the destination *across* travel (insert
  into an existing across-travel group, else split the destination); only the
  very edge spawns a new outside band.
- **Stacks (tabs)** — a box is a **stack** iff one of its children carries
  `selected: true` (the shown one); there is no flag on the box. A stack reports
  the element-wise **max** of all children's boxes (plus a host-supplied header
  reserve) and carries **one** mode / **one** pin of its own (priority stays
  derived), so flipping tabs is provably a repaint, never a relayout. Toggling
  stack copies negotiation state *through* the visible tile so nothing jumps, and
  unstacking re-orients contrary to its container so it can't dissolve.
  `ensureVisible` cycles whatever ancestor stacks are needed to reveal a tile; if
  it still can't fit, it's omitted through the ordinary overflow path.

The module supplies mechanism; the host supplies orientation policy and owns
focus (e.g. `nextFocusAfterDismiss` is a query the host answers).

## Serialization

The **Serialize** panel shows a live JSON dump and round-trips it back on
**load**. The node shape mirrors what the Go module will emit as **PSL** (its
primary format):

- a **tile** is an object keyed by **`ref`** — the only identifier, an opaque
  handle the *host* maps to its content — plus any non-default attributes;
- a **box** is anonymous: `{ "box": "ltr", "children": [ … ] }`;
- **`selected: true`** rides on a *child*; its presence is what makes the
  enclosing box a stack (a stacked box also serializes its own `mode`/`pin`);
- **`focused: true`** also rides on a *child* — a group's remembered last-focus
  child (the focus trail). It can appear on any child of any box; following the
  `selected`+`focused` chain from the root lands on the live focus, and each
  inactive tab keeps its own so returning to a tab restores where you left it.

Defaults are omitted on emit and refilled on load. In PSL the same object is one
named param (`box:` / `ref:` + attrs) plus ordered children; JSON just moves the
children into a `children` array. Example:

```json
{ "box": "ttb", "pin": 200, "enforced": true,
  "children": [ { "ref": "win3", "selected": true }, { "ref": "win4" } ] }
```

## Keys

Arrows navigate; `Shift`+dir = new; `/` then a direction (arrows or WASD) =
split; `'`/`Enter` = new before/after. `WASD` swap, `Shift`+`WASD` merge.
`Z`/`X`/`C` zoom/shrink/cycle mode. `F`/`G` flip group/parent, `R`/`T` reverse
group/parent. `[`/`]` cycle tiles (visible ones only — never inactive tabs).
`−`/`=` grow/shrink. `\` toggle stack, `,`/`.` cycle tabs, `{`/`}` reorder the
active tab. `Delete`/`Backspace` dismiss. On-screen buttons mirror all of these.

Mode/pin/resize and **swap** on a stacked tab act on the whole tab-group (the box
shadows its tabs). **merge** instead moves the single tile: merging a tile *into*
a stack adds it as a new tab, and merging a tab *out* extracts just that tile
(the stack promotes a neighbour and survives) — the two are intuitive inverses.
`split` nests into the active tab; `new` breaks out past *every* tab level to the
nearest non-tab group. Returning to a tab restores focus to where it last was.

When you invoke tab-cycle (`,`/`.`) while *not* inside any stack, it flips the
tabs that are merely visible on screen — one group per press, **without moving
your active focus**. It steps the innermost (deepest, reading order) visible
group that can still move in that direction; a group at its end doesn't wrap, it
hands off to the next group that can. Only when no visible group can advance does
it reset one — a group at the innermost (deepest) level, with direction choosing
first vs last within that level.

A hosted copy of this same page is published as a Claude artifact for quick
sharing, but this file is the source of record.
