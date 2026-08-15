# D2 API-Shape Phase — Tracker

Phase 2 of `graphical-mode-plan.md`: restructure the app-facing API
around the display protocol's proxy/replica model **while everything is
still in one process** — no serialization, no transport. This is the
protocol's dress rehearsal: every breaking API change happens here,
verified against the TUI demo, so the transport phase later is "just"
a second implementation of already-shaped seams.

Constraints honored throughout (from D2/D4): stable IDs instead of
pointer identity; data + events instead of closures crossing the seam;
reads served replica-style (synchronous-looking, cache-backed);
sessions separable from connections.

Additional guardrail (2026-07-05): **the app-side client library is
instance-scoped, never a singleton.** One app must be able to hold N
connections to N display services simultaneously (console + GUI +
remote — the Emacs-daemon scenario), which works by construction as
long as connections are first-class handles, event delivery and
command dispatch carry their originating connection, and nothing
app-side assumes "the one display." ObjectIDs, aliases, templates, and
sessions are already per-connection by earlier decisions; this
guardrail keeps the client library from undoing that.

## Slices

1. **Menu command identity & registry dispatch** — ✅ done 2026-07-05
   (below).
2. **Stable IDs for windows and trinkets** — ✅ done 2026-07-05 (below).
3. **Event-subscription formalization** — trinket callbacks
   (`OnClick`, `OnToggled`, `OnStateChanged`, …) become subscriptions
   keyed by trinket ID under the hood; the public setter API keeps its
   current feel.
4. ✅ **Replicated-read discipline audit** — done 2026-07-05, full
   classification in `d2-read-audit.md`. Three classes: A
   (write-through — the bulk of the surface, incl. item lists the
   app built), B (event-mirrored — every currently-registered state
   event covers its getters), C (server-authoritative: C1 =
   deliberately not app-model state — geometry, resolved
   environment, transient interaction, rendered cells; C2 = six
   enumerated exceptions needing future events: menu checked state,
   window geometry/state events, text caret, multi-select, focus,
   scroll offsets — none blocking transport). Restructured now: the
   demo's menu toggle handlers own their booleans instead of reading
   display-side MenuItem.Checked. The audit ends with the
   four-point veneer contract feeding the client-library phase.

The protocol's wire shape is deliberately NOT decided here, with one
principle now fixed (D10, 2026-07-05): **nothing positional — every
value travels under a property name**, with sender-declared,
connection-scoped alias dictionaries for wire efficiency. Consequence
for this phase: the property and event names formalized in slices 3–4
are wire vocabulary — choose them deliberately and keep a vocabulary
list. (Also validated by the D10 sketch: buttons bind to command IDs
via an `action` property — slice 1's registry extends to buttons in
slice 3.)

## Milestone P0 — the demo runs on the protocol, in-process

Owner-directed goal (2026-07-05): KittyTK and its demo operating on the
D10–D17 protocol basis within a single process — commands and events
as real protocol records, no sockets yet. Steps:

1. ✅ **Protocol core package** (`protocol/`) — parser + AST for the
   full command language: flags (`!`/`?`), quoted strings, numerics,
   identifiers, `{}` blocks, keyed statements, alias/template forms,
   surfacing references. The tokenizer is schema-free per D17; alias
   application and template expansion belong to the layers above.
   Provisional pending O6: `#` comments; string escapes `\\ \" \n \t
   \r`. Eight tests cover the canonical corpus from D10–D17.
2. **Vocabulary binding + builder** — split in two:
   - ✅ **2a. Protocol semantics** (`protocol.Session`, 2026-07-05):
     executes scripts against a `Factory`/`Object` interface pair —
     alias declaration/substitution (uppercase-required, string
     targets), template declaration/expansion (macro semantics,
     transitive with cycle guard, children components, instance
     overrides incl. flag un-set), children blocks, hierarchical key
     registration with explicit surfacing, D18 case enforcement,
     correlation replies (surfaced names + top-level keys only). Ten
     tests with a mock factory cover the D10–D18 semantics corpus.
   - ✅ **2b. Trinket-owned wire registration** (2026-07-05).
     Architecture per owner directive: **each trinket's own codebase
     registers its type and property mappings** in a sibling
     `*_protocol.go` file — no central binding table. The registry
     machinery lives in `protocol` (`RegisterType` /
     `RegisterCommonProperty` / `RegistryFactory` / `BindContext` +
     the D17 typed-conversion helpers); `trinkets/protocol_reg.go`
     provides shared applier helpers and registers the common
     properties (enabled, visible, name, min/max sizes,
     column_units/row_units, font, acc_name). Registered v1 types:
     button, label, checkbox (tri-capable `checked`), radiobutton,
     textinput, combobox + virtual `item` (D13 children unification),
     panel (border/border_style/layout/spacing), splitter, spacer,
     separator, progress. `action=` wires into the connection's
     command dispatcher via `BindContext` (instance-scoped per the
     multi-display guardrail). Also fixed audit finding #7 for real:
     `Panel.SetBorder(true)` now defaults to a visible single-line
     style. Six end-to-end tests build real trinket trees from
     protocol text (aliases, templates, tri-state, surfacing to real
     ObjectIDs, live command dispatch on click). Deferred: window/
     tabs/lists/trees/menus types; `set` verb and friends (verb
     inventory — O6 item for the owner); radio `group`; items={}
     spelling reconciliation (v1 uses children per D13).
3. ✅ **Event records + subscriptions** (absorbs slice 3; 2026-07-05).
   `protocol.Event` follows the same named-property discipline as
   commands — `event toggle trinket=17 ?checked` — and its encoded
   form is a parseable statement, so one tokenizer serves both wire
   directions (Encode/ParseEvent round-trip tested, incl. string
   escaping). Emission is trinket-owned like everything else: each
   trinket's `*_protocol.go` wires its callbacks in a `TypeSpec.Bind`
   hook called once at construction — button `click`, checkbox
   `toggle` with tri-state flags (D16 on the wire), radio `toggle`,
   textinput `change text=`, combobox `change selected=`. Events
   flow through `BindContext.Emit` (per-connection; nil-safe);
   `action=` became per-trinket *data* (`BindContext.SetAction` /
   `FireAction`), so assigning or replacing an action never re-wires
   callbacks — activation dispatches the command registry AND emits
   a `command` event. App side: `protocol.EventDispatcher` routes by
   ObjectID + event type (`On`) or type-wide (`OnType`). In-process
   delivery is synchronous by default; a channel is just an Emit
   that enqueues (transport decides later). Known/accepted:
   construction-time property application emits state events
   (suppression policy joins the `sub` verb decision). Deferred:
   window events, key events (raw-key mode), the `sub` verb itself
   (v1 emits for all bound trinkets).
4. ✅ **Demo on this basis** (2026-07-05). The demo app opens a
   "Protocol Demo" window at startup whose entire content — panel,
   wrapping label, status label, separator, tri-state checkbox,
   textinput, combobox with items, action-bound button — is built by
   executing protocol text (`protocolWindowScript` in
   `examples/demo/main.go`), exercising aliases, children blocks,
   flags, and hierarchical surfacing (`watch=root.status`). App-side
   handlers subscribe via `protocol.EventDispatcher` keyed by the
   surfaced ObjectIDs; interacting with the trinkets updates the
   status label with the received event, and the button's
   `action=demo.hello` lands in `Application.Commands()` (slice-1
   seam) *and* arrives as a `command` event. A demo-package test
   (`TestProtocolWindowScriptBuilds`) guards the script + surfaced
   keys, since `createProtocolWindow` degrades to "no window" on
   error. Writing it caught a real bug: virtual items drew IDs from
   a protocol-private counter that collided with `core.ObjectID`s —
   fixed with `protocol.SetVirtualIDSource`, which `trinkets` points
   at the same allocator as real trinket IDs (`core.NextObjectID`).

**Milestone P0 complete (2026-07-05):** KittyTK and its demo run on the
D10–D18 protocol basis in-process — UI built from protocol text,
interactions delivered as protocol event records, commands dispatched
by stable ID.

## Post-P0: verbs + vocabulary completion *(done 2026-07-05)*

Owner decisions D19/D20 (see graphical-mode-plan.md) implemented in
one slice:

- **Verbs**: `set` (mutate by key path or numeric ID; accepts
  everything `new` does incl. `children={}` appends), `destroy`
  (detaches via per-type Destroy hooks — a window closes, a trinket
  leaves its parent — and releases keys), `sub`/`unsub`
  (`sub <target>|all [events…]`). Correlation keys became
  session-persistent; surfacing registers the surfaced name as a key.
  Bare numbers are legal ONLY as verb targets (parser emits anonymous
  args; the session rejects them in property position).
- **Event flow (D20)**: default-closed except `command` —
  `BindContext` holds the per-connection subscription table;
  `EmitEvent` filters; `command` events and registry dispatch always
  flow. Wire-initiated mutation (`new`, `set`) runs under
  `BindContext.Suppressed`: no state-event echo, no action firing.
  `EventControl` is the optional Factory capability carrying
  sub/unsub/suppression; wrappers must forward it.
- **Vocabulary registered**: `tabs`+virtual `tab` (position enum,
  selected, change events), `listview`, `treeview` (both consuming
  the now-shared virtual `item`, which nests for trees; change/
  activate events), `scrollarea` (single content), `window` (title,
  bounds, D12 behavior flags, window_closed event, destroy=close; in
  the window package per trinket-owned registration), radiobutton
  `group=` (connection-stash groups — no container trinket), common
  `stretch`/`align` (layout hints on the child, consulted by
  BoxLayout at attach), common `fg`/`bg` colors (named words +
  quoted "#rrggbb").

**Tree-item identity (2026-07-05):** items are addressable wire
objects — the owner's ruling: identity is the ObjectID (items already
drew from the trinket ID space), and the "user-chosen unseen name" is
the existing correlation-key system, not a new mechanism. The wire
item record becomes a live proxy when a treeview adopts it
(`wireItem.bind` carries the ID onto the `TreeItem` and keeps
backrefs), so `set tree.fruit …`, `set … children={}` (live append),
and `destroy tree.fruit` mutate the visible tree; selection events
carry `item=<id>`, and user expand/collapse emits
`expand item=<id> expanded|!expanded`. `TreeItem.ID` is auto-assigned
Go-side too. Factory hook: virtual targets implementing
`SetWireID(uint64)` learn their identity at construction.

**Main demo window converted (2026-07-05):** the "TUI Toolkit Demo"
window IS protocol text now (`examples/demo/mainwindow_protocol.go`).
Eight tabs — Basic Trinkets, Selection, Lists, Scroll Selection,
Scroll Lists, Progress, Bottom Tabs, Vertical Tabs — build from one
script (window + tabs + nested splitters/scrollareas/lists/trees;
repetitive runs generated, still protocol text). The MDI Demo tab
stays imperative by design (G1 residuals + PurfecTerm) and attaches
through the surfaced TabTrinket — the supported hybrid. App-side
wiring is entirely protocol-shaped: buttons dispatch commands via
`action=`, the text input and all font/denomination/background
toggles arrive as subscribed events keyed by surfaced ObjectIDs, and
~800 lines of imperative construction are deleted. The demo also
registers its own wire type (`fixedbox`), proving app-local
vocabulary extension works through the public API. Paint tests
render both the Basic Trinkets and Selection tabs from the script —
the tab flip in the test is done with `set tabs selected=1`.
Found and fixed during conversion: label's `align` (text alignment)
shadowed the common layout `align` hint — renamed `text_align`.

**Menus + terminal feed (2026-07-05):**

- **Menus are protocol data (G6 delivered):** `menubar`/`menu`/
  `menuitem` types registered; submenus grow from menuitem children.
  Both demo menu bars (primary + secondary apps) are scripts now;
  every handler moved to `Commands().Register` under `action=` IDs —
  the last closure wiring in the menu path is gone. Checkable-state
  reads go through surfaced items.
- **Terminal `feed`:** the string escapes gained `\e` and `\xNN`, so
  arbitrary byte streams travel as quoted strings today (event
  encoding round-trips control bytes too); the O6 bulk frame becomes
  a transport-phase encoding optimization of the same statement.
  `terminal` registered with `feed=` (append/stream pseudo-property
  over `PurfecTerm.Write`) and the in-process `shell` flag. The
  revived Demo Window (Demo → New, `demo.file.new`) is built from
  protocol text, banner fed over the wire before the local shell
  starts, close button subscribed per-connection so instances never
  collide.

## Client-library veneer *(done 2026-07-05)*

The `client` package implements the slice-4 veneer contract — the
app-facing purity layer P0 deferred:

- **`client.Conn`** — instance-scoped (multi-display guardrail),
  imports ONLY `protocol`: it compiles with zero knowledge of the
  rendering side. `NewInProcess(dispatch)` wires the in-process
  display (registered vocabulary via `RegistryFactory`); a remote
  transport later slots in behind the same surface.
- **Replica reads**: `Conn` interposes a recording factory (types +
  in-process targets) and folds subscribed events into per-object
  state BEFORE app handlers run — `Checkbox.State()`,
  `TextInput.Text()`, `Selector.Selected()` are synchronous and
  never cross the wire.
- **Write-through setters**: `SetChecked`/`SetText`/`Select`/
  `SetCaption` update the replica and send `set <id> …`; D20
  guarantees no echo (tested: zero events observed on writes, user
  edits still flow).
- **Typed handles** from `Build(script)`: `Button` (OnClick),
  `Checkbox` (tri-state), `TextInput`, `Selector` (combobox/list/
  tree/tabs share the shape), `Label`, `Window`, generic `Handle`
  with `Set(raw)` escape hatch, `Destroy`, `On(event)`. Handles
  auto-subscribe the events backing their replica getters; class-C1
  reads (geometry etc.) deliberately do not exist on handles.
- **In-process escape hatch**: `Handle.Target()` exposes the real
  trinket for hybrid apps (window managers, AddWindow); documented as
  nil under future remote transports.
- The demo's Protocol Demo window is the showcase: no raw
  dispatcher, no sub statements in the script, handlers write back
  through the veneer (`status.SetCaption`). Six tests cover replica
  mirroring, no-echo, fold-before-handler ordering, command
  observation, escape hatch + destroy, and connection independence.

**Messagebox + status bar (2026-07-05):** `messagebox` registered
(buttons as individual D12 flags, `finish result=` event,
destroy=close); all three demo dialogs are scripts via
`protocolMessageBox`. Status bar content is protocol data:
`statusbar`/`section`/`span` virtuals — the first inline styling on
the wire (span `fg=`/`bg=`) — used for the main bar, the secondary
apps' bars, and the dynamic cell-debug readout (built per-update
with `protocol.Quote`, the new exported string-literal quoter).

**MDI tab converted (2026-07-05):** `mdipane` and `dockrow`/
`dockentry` registered; the tab is a script + veneer handlers.
Spawning documents is `set mdi children={new window …}` (D19 append
of a whole window subtree); Tile/Cascade/Next/Prev are flag-action
properties and restore/minimize/remove are id-directed actions; the
dock choreography (minimize event → dockentry, entry click →
restore, restore/remove → destroy entry) runs entirely over
pane/dock events with per-child buttons on click events — no
command-ID collisions. ~250 lines of imperative MDI wiring deleted.

Remaining: `state` window property, listview row set/destroy
routing, terminal input direction (`data` events, raw-key) — the
demo's only remaining imperative UI is the secondary app's window
body.

## Slice 1 — Menu command identity & dispatch  *(done 2026-07-05)*

What exists now:

- **`core.CommandRegistry`** — handlers keyed by stable string command
  ID; `Register` / `Unregister` / `Has` / `Dispatch`. The in-process
  half of the dispatch seam: under the protocol, the display service
  emits "command <ID> triggered" events and the app-side client
  library dispatches through exactly this shape.
- **Every `MenuItem` has a stable ID** — auto-assigned
  (`cmd.auto.N`) at construction; override with `SetID` for semantic,
  run-stable IDs (`"file.open"` — the `core.StandardActions`
  vocabulary predates this and fits directly).
- **`Menu.BindCommands(reg)`** walks a menu tree (submenus included),
  registers each item's handler under its ID, and routes all future
  triggers through the registry. `MenuItem.Trigger()` dispatches by ID
  when bound, falling back to the direct closure when not (standalone
  menus keep working).
- **Wiring:** `Application.SetMenuBarContent` binds automatically into
  the app's registry (`Application.Commands()` accessor); the Desktop
  binds its system menu into a desktop-level registry. The shortcut
  path (`checkMenuItemShortcuts`) now goes through `Trigger()` like
  every other activation path — closures are no longer invoked
  directly anywhere.
- Behavior notes: shortcut activation of a checkable item now toggles
  its checked state, consistent with clicking (previously it bypassed
  the toggle). `SetOnTriggered` after binding refreshes the
  registration.

Public API unchanged: `NewMenuItem("&Open").SetOnTriggered(fn)` works
exactly as before; `SetID` is additive.

Deferred within this slice (tracked, not forgotten):

- Standard items injected by `createAppMenuWithStandardItems`
  (Hide/Quit) use the closure fallback; bind them to the desktop
  registry when that merge path is next touched.
- `core.Action` integration: `MenuItem` and `Action` should
  eventually converge (an item constructed *from* an action inherits
  ID/shortcut/enabled/checkable) — slice 3 territory.
- Dock entries (`OnClick`) and `PopupRequest` callbacks are the other
  closure-crossing surfaces; they join in slices 2–3.

## Slice 2 — Stable object identity  *(done 2026-07-05)*

What exists now:

- **`core.ObjectID`** (uint64) — the stable identity of a UI object.
  Allocated from a process-wide counter at `NewTrinketBase()`, so every
  trinket, window, panel, and the desktop itself carries one from
  birth: `w.ObjectID()`. Immutable after construction.
- **Deliberately NO process-global ID→object registry.** The object
  table belongs to the display service's per-session connection state
  (created at attach, released at detach, per D4's session model).
  A global registry now would bake in the wrong lifecycle and leak
  discarded trinkets; the transport phase builds the real table.
- **First consumers converted (both fixed latent identity bugs):**
  - Dock entries carry `WindowID`; minimize/restore wiring (Desktop
    and Application) adds/removes by ID. Previously keyed by window
    *title* — two same-titled windows corrupted the dock.
    `RemoveEntryByTitle` is deprecated but kept.
  - ComboBox popup IDs derive from `ObjectID()`. Previously
    `"combobox-" + Name()` — unnamed comboboxes collided.
- Distinction now explicit in the codebase: **ObjectID is identity;
  `Name()` is a human label; command IDs are semantic verbs.** Three
  different things, no longer substitutable.

Next: slice 3 (event-subscription formalization) keys trinket-event
subscriptions by ObjectID — the "trinket 17 was clicked" half of the
event stream, joining slice 1's "command file.open triggered" half.
