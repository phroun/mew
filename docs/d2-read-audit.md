# D2 Slice 4 — Replicated-Read Discipline Audit

Date: 2026-07-05. Scope: every public getter on the wire-registered
types (and the trinket-base surface behind them), classified by where
its answer can come from once a transport separates app from display
service. The constraint (D2/D4): reads must look synchronous to app
code, which means they are served from an **app-side replica** — the
client library's cache — never by a blocking round-trip.

## The classification

Three classes, by what keeps the replica true:

- **A — write-through.** State only the app ever changes. The client
  library records the value when the app sets it; the getter returns
  the cache. Nothing crosses the wire to read. This is the largest
  class by far.
- **B — event-mirrored.** State the *user* changes through the
  display service. True in the replica exactly when the mirroring
  event exists AND the client library subscribes to it. Rule for the
  veneer: **the client library auto-subscribes to every event that
  backs a B-getter it exposes** (D20's default-closed applies to app
  handlers, not to the library's own replica maintenance).
- **C — server-authoritative.** Derived or transient display-side
  state. Either (C1) legitimately not part of the app-facing model —
  don't expose it across the wire at all; or (C2) something apps do
  need, which therefore requires an event or an explicit async query.
  Every C2 is an exception to restructure; they are enumerated below.

## Class A — write-through (no wire traffic to read)

All construction/styling properties on every registered type:
captions/text labels, `placeholder`, `wrap`, `tristate`, `checkable`,
`enabled`, `visible`, `name`, `readonly`, `mask`/echo mode,
`max_length`, min/max sizes, `stretch`/`align` layout hints, fonts,
`column_units`/`row_units`, `fg`/`bg`, panel `border`/`border_style`/
`layout`/`spacing`, splitter `orientation`/`caption`, tabs
`position`/`movable`/`closable`, listview `alternate_rows`, treeview
`indent_width`, progress `minimum`/`maximum`/`caption`/
`indeterminate`, spacer size, window `title` + behavior flags,
messagebox `title`/`text`/`icon`/button set, menu structure
(captions, shortcuts, separators, submenus, `action` IDs), status
bar content, radio `group` membership.

Also class A despite looking like queries: `Count()` /
`ItemText(i)` / `Items()` / `RootItems()` — the item lists are what
the app built (or grew via `set … children={}`); the replica holds
them by construction. `progress value` is A when app-driven (the
normal case).

## Class B — event-mirrored (replica true given subscription)

| State | Getter(s) | Mirroring event | Status |
|---|---|---|---|
| Checkbox state | `IsChecked`, `CheckState` | `toggle ?checked` (tri-state D16) | ✅ exists |
| Radio state | `IsChecked` | `toggle checked/!checked` | ✅ exists (group exclusivity derivable: one `checked` implies siblings off) |
| Text content | `TextInput.Text` | `change text=` | ✅ exists |
| Combo selection | `CurrentIndex`, `CurrentText` (= items[selected], derived A+B) | `change selected=` | ✅ exists |
| List selection | `CurrentIndex`, `CurrentItem` | `change selected=`, `activate` | ✅ exists |
| Tree selection | `CurrentIndex`, current item ID | `change item= selected=`, `activate` | ✅ exists |
| Tree expansion | `TreeItem.Expanded` | `expand item= expanded/!expanded` | ✅ exists |
| Tab selection | `CurrentIndex` | `change selected=` | ✅ exists |
| Dialog result | `MessageBox.Result` | `finish result=` | ✅ exists |
| Window closed | — | `window_closed` | ✅ exists |
| Button pressed-ness | (momentary) | `click` / `command` | ✅ exists (not state; fire-and-observe) |

## Class C — server-authoritative

### C1 — deliberately NOT app-model state (do not expose over the wire)

- **Geometry**: `Bounds`, `Pos`, `Size`, `SizeHint`, `MinimumSize`
  (computed), `HeightForWidth`, `ClientArea`, splitter pixel
  position. Layout is the display service's job (D2); apps declare
  constraints (class A), the service computes placement. An app that
  reads `Bounds()` is layouting by hand — the API smell slice 4
  exists to catch.
- **Resolved environment**: `EffectiveFont`, `EffectiveCellMetrics`,
  `EffectiveScheme`, `Theme`, effective background — resolution
  climbs the display-side tree.
- **Transient interaction state**: `ComboBox.IsOpen`, popup
  visibility, drag/resize in progress, `AnimatePress`,
  hover. Ephemeral by nature; an app that needs them is scripting
  the UI, not using it.
- **Rendered content**: terminal cells (`GetCells`), scrollback.
  The display IS the authority; `feed` is one-way by design.

### C2 — exceptions: apps legitimately need these; each needs a
mirror event or must move to class A

1. **Menu checkable state** (`MenuItem.Checked`). `Trigger` toggles
   display-side, then dispatches the command — but the command
   carries no state, so a handler reading `item.Checked` crosses the
   seam. **Restructured now (see below): the demo's handlers own
   their booleans (class A — the toggle intent is the app's state).**
   Protocol follow-up when menus move display-side for real: the
   `command` event gains `?checked/checked/!checked` for checkable
   items.
2. **Window geometry & state after user interaction** (`Bounds` after
   drag, `IsMaximized`, `IsMinimized`). Vocabulary already reserves
   `window_moved`/`window_resized`/`window_state`; the Window trinket
   has no callbacks to emit them from yet. Tracked: add callbacks +
   Bind emission with the window-events slice.
3. **Text cursor / selection** (`CursorPosition`, `SelectedText`,
   selection range). `change` carries `text=` only. Fine for v1
   (most apps only need text); if an app needs caret tracking, add
   `cursor=` to `change` or a `caret` event. Deferred, recorded in
   the vocabulary doc.
4. **Multi-select lists** (`SelectedIndexes`, `IsSelected`). No
   selection-set events yet — deferred WITH the `multi_select`
   feature itself; single selection is fully mirrored.
5. **Focus** (`HasFocus`). `focus_in`/`focus_out` are in the event
   vocabulary but nothing emits them (trinkets have Handle methods,
   not callbacks). Tracked with the raw-key/input slice, which needs
   focus routing anyway.
6. **Scroll offsets** (`ScrollArea` X/Y after user scrolling).
   Writable over the wire; reads are display-side. Add a `scroll`
   event if an app ever needs to track it; none does today.

## Restructures applied in this slice

- **Demo menu handlers** (`demo.view.announce` / `demo.view.speak`):
  previously read `item.Checked` after trigger (a C2-1 read). Now
  each handler flips an app-owned boolean — the app's replica of the
  toggle intent — which stays consistent with the display-side check
  mark because both flip on the same activation. This is the
  replica discipline applied to our own dogfood.

## The veneer contract (input to the client-library phase)

1. Every setter records into the replica (class A) before/while the
   `set` goes on the wire (fire-and-forget, D2 async writes).
2. The library auto-subscribes to all class-B events for objects it
   holds handles to, and folds them into the replica before invoking
   app handlers.
3. Class C1 getters do not exist on the client-side handle types.
   (In-process trinkets keep them — display-side code needs them; the
   veneer simply doesn't mirror them.)
4. Each C2 item graduates to B by adding its event; none blocks the
   transport phase.
