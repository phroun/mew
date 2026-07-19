# Interactive UI Builder — Plan (no code yet)

Status: idea/plan for review. Written to orient a future contributor (human
or AI) before any implementation. Grounded in the protocol and trinkets as
they exist today; every claim below names the code that backs it.

## The one-paragraph vision

A KittyTK client application ("the builder") for visually composing UIs. A
toolbar of available parts across the top; below it, left side stacked
top-to-bottom: an object TREE of everything built so far, and a PROPERTY
INSPECTOR for the selected object (custom props, then common props, grouped
by category, with explicitly-set values visually distinct from resting
defaults). The whole right side is a scroll area holding a LIVE preview of
the UI being built. Any subtree can be "componentized" — captured as a named
reusable part that joins the toolbar.

## Three grounding facts that do most of the design for us

The builder is not a greenfield tool; the protocol was shaped (D10-D24) so
that this tool falls out of it. Three existing mechanisms carry the load:

### 1. The document format already exists: it is the wire script

`examples/demoapp/scripts.go` builds the entire nine-tab demo as a single
declarative text script — `new window ... children={ new tabs children={ ... } }`
— executed verbatim over the socket. So the builder's save file is not a new
format to invent: **a builder document IS a protocol script** (suggested
extension: `.ktk`). Saving is serialization of the document model to `new`
statements; "run my UI" is any client replaying the file byte-for-byte. No
importer/exporter pair, no schema drift between "design format" and "real
format" — they are the same text. Round-tripping is trivially lossless
because the builder's in-memory model is defined as exactly the information
in that text (type, props explicitly set, children order, correlation keys).

### 2. The inspector already has its data source: `describe`

The host self-documents (D24): `client.Conn.Describe()` returns a
`protocol.Vocabulary` — every registered type, each with its type-specific
properties, plus the common properties, and for every property its
`Kind` (string, int, float, flag, enum, word, color, units, stream, action),
its `Default` (a literal, or "inherited"/"as-noted"), a tooltip-length `Doc`,
and the `Enum` word list where applicable (`protocol/describe.go`).

Consequence: the property inspector is **generated, not authored**. The
builder hardcodes zero per-widget knowledge. Kind→editor mapping:

| Kind          | Editor                                              |
|---------------|-----------------------------------------------------|
| string        | textinput                                           |
| int / float / units | textinput with numeric validation             |
| flag / bool   | checkbox (indeterminate = "unset/default", see below)|
| enum / word   | combobox from `Enum`                                |
| color         | textinput in v1 (swatch popup later)                |
| action        | textinput (command id) — see events, below          |
| stream        | not editable in the inspector (runtime data)        |

New trinket types and properties registered in the future appear in the
builder automatically — the tool never goes stale.

### 3. Componentization already exists: it is the `template` verb

`template Name=base props... children={...}` (D14/D18,
`protocol/session.go: declareTemplate`) defines a named part; template
children concatenate ahead of instance children at instantiation; expansion
is transitive with a cycle guard; user template names MUST begin uppercase,
so builtins (lowercase) and user components (uppercase) are disjoint
namespaces by construction — the toolbar can mix them without collision.

So "componentize this subtree" = serialize the subtree as a `template`
declaration and prepend it to the document. The toolbar's palette is
literally: the lowercase types from `Describe()` + the uppercase templates
declared in the current document (plus a user library file of templates —
which is just another `.ktk` script of `template` statements).

## The builder is itself a KittyTK client (dogfooding)

The builder connects to a display like any app (`client` package) and builds
its own chrome from the same trinkets it edits. Everything its layout needs
already exists as a registered wire type: `window`, `panel` (vbox/hbox),
`splitter`, `treeview`/`item`, `scrollarea`, `button`, `textinput`,
`checkbox`, `combobox`, `label`, `tabs`, `menubar`, `statusbar`.

Sketch (all existing types):

```
window "UI Builder"
└─ panel vbox
   ├─ panel hbox                        ← TOOLBAR (palette of parts)
   └─ splitter horizontal position≈0.3
      ├─ splitter vertical position≈0.5
      │  ├─ scrollarea > treeview       ← OBJECT TREE
      │  └─ scrollarea > panel vbox     ← PROPERTY INSPECTOR (generated rows)
      └─ scrollarea                     ← LIVE PREVIEW CANVAS
         └─ (the document, instantiated)
```

Notes:
- There is no toolbar trinket today; a `panel layout=hbox` of buttons is
  fine for v1 (a real `toolbar` type with overflow can come later).
- There is no property-grid trinket; the inspector is a generated vbox of
  rows (label + editor). If that proves clumsy, a two-column `propertygrid`
  trinket is a natural later addition — but do not start there.

## The document model (the builder's single source of truth)

A small in-memory tree, one node per object:

```
Node { type (or Template name), key?, props: ordered map name→literal,
       children: []Node }
Document { templates: []TemplateDef, roots: []Node }
```

Two invariants make everything else simple:

1. **The document is authoritative; the live preview is a projection.**
   The protocol deliberately has no `get` verb (write-only wire, events
   flow back) — and the builder never needs one, because it is the sole
   author of everything on the canvas. No read-back, no reconciliation.
2. **`props` holds ONLY what the user explicitly set.** This is the entire
   mechanism behind the explicit-vs-default distinction:
   - explicitly set  = the property has an entry in `props`
     → inspector renders it emphasized (normal/bold), with a "reset"
       affordance that simply deletes the entry.
   - resting default = no entry → inspector renders the vocabulary's
     `Default` greyed/dimmed.
   Serialization writes only `props`, so saved files stay minimal and
   diff-able, exactly like the hand-written demoapp scripts.

## Live preview synchronization

The builder holds one connection; the preview lives inside its own canvas
scrollarea (correlation keys map document nodes → live object IDs — `k=new`
returns IDs in the reply, `protocol/session.go`).

- **Property edit** → `set <key> name=value` on the live object. Cheap,
  immediate. (`set` reaches every registered property applier.)
- **Structural edit** (add/move/delete/reorder) → `destroy` the enclosing
  subtree's live objects and re-`new` from the document. The wire already
  supports incremental child-append (`set <key> children={...}` — applyArgs
  handles children for `set` too) and `destroy`; there is no reorder verb,
  so reorder = rebuild the parent. Rebuild-subtree is the v1 answer for ALL
  structural edits: simple, always correct, and fast at UI scale (the whole
  nine-tab demo builds in one batch today).
- Wire-initiated mutations are echo-suppressed by design (D20), so the
  builder's own edits never bounce back as events.

## Selection and "design mode"

v1: selection happens in the TREE (and tree→canvas highlight can wait).
This defers the one genuinely new mechanism the preview needs: clicking a
live button on the canvas activates it instead of selecting it.

v2: design-mode picking. Grounded options, in order of preference:
1. An **overlay trinket** stretched over the canvas that captures mouse
   events, hit-tests the document's geometry, and selects — the desktop
   already routes events through filters (`Desktop.dispatchEvent` /
   `filterEvent`), and the builder owns the preview subtree, so a
   transparent capture layer is a local, non-protocol change.
2. A `design` flag property on a container that makes descendants inert.
   More invasive (touches every trinket's event path); avoid unless (1)
   proves insufficient.
Selected-object adornment (marching ants / handles) is also v2; the tree
highlight covers v1.

## Componentize flow (concrete)

1. User right-clicks a tree node → "Save as component…", names it `MyCard`
   (builder enforces the uppercase rule, D18).
2. Builder serializes that subtree into
   `template MyCard=panel <explicit props> children={...}`, appends it to
   `Document.templates`, replaces the original subtree node with a
   one-node `MyCard` instance, and adds `MyCard` to the toolbar.
3. Instances of `MyCard` are single nodes in the tree; their inspector
   shows the BASE type's properties (a template is prop-presets over a base
   type — overriding an instance prop just sets it on the instance, which
   the wire already resolves as instance-args-after-template-args).
4. A component library = a plain `.ktk` file of `template` statements the
   builder loads at startup. Nothing new to invent.

Limitation to document honestly: templates parameterize by *overriding
props on the root base type*; there is no "slot"/placeholder mechanism for
overriding a nested child's caption per-instance. That is a real future
protocol conversation, not a builder hack. v1 components are "stamps";
editing one means editing the template. The TrinketClass proposal below is
the candidate answer.

## TrinketClass — user-defined classes as first-class vocabulary (proposal)

Owner's idea, recorded here for refinement: attach a metadata class name to
any node in the tree, and introduce a `trinketclass` object — a container
whose children are PROPERTY objects, each carrying exactly the metadata our
builtin properties already carry (`PropDesc`: kind, default, doc, enum).
Business logic for how the class behaves attaches either display-side or
app-side.

### Why this is the right shape

It closes the template limitation above by giving a component its own
declared property *surface*, and — the elegant part — because a class's
properties use the SAME descriptor shape as builtins, the whole
introspection pipeline lifts for free: `describe` can report user classes
alongside builtin types, and the builder's generated inspector cannot tell
the difference. User components stop being second-class the moment they are
declared.

Every mechanism it needs has an existing precedent:

- **Descriptor-holding children**: virtual types are the established
  pattern for "objects that exist to carry structured data" — `item`,
  `section`/`span`, `menuitem`, `dockentry` (`TypeSpec.Virtual`,
  `objects/trinkets/statusbar_protocol.go`). A `classprop` is a virtual
  object whose payload IS a `PropDesc` plus a binding.
- **Session-scoped declaration**: templates already live in the session
  (`s.templates`), declared over the wire, uppercase-named (D18). Classes
  are the same lifecycle: declared in the document/connection, saved as
  statements at the top of a `.ktk` file.
- **Instantiation & inner addressing**: template expansion at `new` (D14)
  plus correlation keys already give the machinery to instantiate a
  subtree and address nodes inside it (`k=new`, key paths in `set`).

### Wire sketch (illustrative, names not frozen)

```
new trinketclass name=Card base=panel children={
    new classprop name=title kind=string default="" doc="Card heading" bind=hdr.caption
    new classprop name=level kind=enum enum=info,warning default=info doc="Accent" bind=hdr.scheme
    new classprop name=ref   kind=string default="" doc="App-side record id"        # no bind: pure state
    new classevent name=dismissed from=closer doc="User dismissed the card"
    new classbody children={
        hdr=new label
        new spacer
        closer=new button caption="Dismiss"
    }
}

c1=new Card title="Hello" level=warning ref="ticket-42"
```

Semantics: `new Card` instantiates the body subtree; setting a class prop
resolves its `bind` (a key path relative to the instance root) and forwards
the value to the inner property; a prop with no `bind` is stored state that
rides along on the instance's events. `classevent from=` surfaces an inner
object's event under the class's own name, so the app subscribes to `Card
dismissed` without knowing the internals.

### Where the business logic lives — recommendation

Split it by NATURE, using the line the architecture already draws (the
render server draws; the process belongs to the app — see the client-side
pty decision):

- **Display-side: declarative only.** Property bindings (class prop →
  inner props, possibly one-to-many, possibly with a value map like
  `false→white / true→red`), and event surfacing (inner event → class
  event). This is data flow, not computation — no conditions, no loops, no
  scripting language on the display server. It keeps the server thin,
  introspectable, and safe (a display accepting remote connections must
  not grow an embedded interpreter).
- **App-side: everything imperative.** Real behavior arrives exactly as it
  does today — the class instance emits its (surfaced) events, the app
  handles them in its own language and responds with `set`. A class is a
  *facade*: schema + wiring on the display, brains in the app.

If genuinely computed display-side behavior is ever wanted (validation,
derived values), that is a separate, deliberate protocol conversation —
start with bindings and see how far they carry; the demo suggests very far.

### The light form: `class=` as a plain annotation

"Attach a metadata class name to any item" also has a useful weak reading:
a common `class=` word property that is pure annotation — no behavior. It
gives the builder a grouping/selector handle, test tooling a stable target,
and theming a future hook (CSS-class-like). Cheap, orthogonal to
`trinketclass`, and worth having even if the full proposal waits. The
builder's "promote to class" gesture then reads naturally: tag the subtree,
generate the `trinketclass` declaration from it, swap in an instance.

### Open design points (for the next session on this)

- Naming: `trinketclass`/`classprop`/`classevent`/`classbody` vs shorter
  (`class`/`prop`/`event`/`body`)? Lowercase verbs are system vocabulary
  (D18) — the CLASS NAME is the uppercase user identifier.
- Does `trinketclass` subsume `template`, or layer on it? Leaning: keep
  template as the simple stamp; class = template + declared surface.
- Two-way bindings (inner state change → class prop event) — needed for
  form-like components; defer until a concrete case demands it.
- Can a class base be another class? Templates already chain transitively
  with a cycle guard; reuse that rule.
- `describe` reporting: session-scoped classes appear only on the
  connection that declared them — is that the right visibility for a
  multi-app desktop?

## Inspector grouping ("by category")

`PropDesc` has Kind/Default/Doc/Enum but **no category field** — this is
the one small, genuinely missing piece of protocol surface. Two options:

- **Preferred:** add an optional `Cat(string)` fluent builder alongside
  `Tip/Def/OneOf` (`protocol/describe.go`) and a `cat=` arg in the describe
  stream. `DecodeVocabulary` reads named args, so old clients simply ignore
  it — backward compatible. Categories then live where the rest of the
  self-documentation lives: at registration, in each trinket's
  `*_protocol.go`. Suggested starter set: `content`, `layout`,
  `appearance`, `behavior`.
- Fallback (no protocol change): client-side heuristic grouping by name
  (width/height/align/stretch → layout; color/font → appearance; …). Works
  day one, but knowledge lands in the wrong place; use only as a stopgap.

Common properties render in their own section below the type-specific ones,
matching how `describe` already partitions them.

## Action/event wiring in the builder

`action=` is already a first-class property kind. The inspector edits it as
a plain command-id word; the saved script carries it. The builder does NOT
try to be a code editor: wiring commands to behavior stays in the consuming
app. (A "test mode" toggle that temporarily lifts design-mode so the user
can feel the UI is a cheap, worthwhile toolbar button.)

## What does NOT need building (anti-scope)

- No new document format, parser, or DSL — the wire script is all three.
- No `get`/read-back protocol work — document model is the truth.
- No per-widget inspector forms — generated from `Describe()`.
- No component runtime — `template` is the runtime.
- No undo *system* invention: undo = snapshots of the (small) document
  model; the preview re-projects. Do it from day one, it is ~free.

## Phased plan

Each phase is independently shippable and testable headlessly (the existing
pattern: drive a `Session` directly or a display over a socketpair, assert
on the document text and the built trinket tree — see
`examples/demoapp/integration_test.go`).

1. **Document core.** Node/Document model; parse a `.ktk` script into it
   (reuse `protocol.Parse`); serialize back out (only explicit props;
   stable ordering). Round-trip tests against `demoapp/scripts.go` content.
2. **Shell + tree + preview.** Builder app shell (layout above); load a
   document; project it to the canvas; tree reflects it; tree selection
   only. Structural edits via toolbar (add child of selected, delete);
   rebuild-subtree sync.
3. **Inspector.** `Describe()`-generated rows; kind→editor mapping;
   explicit-vs-default rendering + reset; `set`-based live updates.
   (Do the `Cat()` protocol addition here.)
4. **Components.** Componentize flow; template section in saved files;
   toolbar shows document + library templates.
5. **Design-mode picking + polish.** Canvas click-select overlay, canvas
   selection adornment, drag-reorder in the tree, test-mode toggle.

## Open questions for the project owner

- Save-file conventions: one window per document, or allow multiple roots
  (the wire allows several top-level objects — demoapp adopts window +
  menubar + statusbar as app chrome)?
- Should the builder emit correlation keys for every node in saved files
  (stable handles for the consuming app), or only where the user names one?
- `Cat()` categories: agree on the starter vocabulary before Phase 3.
- Components-with-slots: the TrinketClass proposal (section above) is the
  candidate mechanism; decide whether it lands with the builder's Phase 4
  or after real template usage exposes the sharpest needs.
- Reorder verb (`set ... index=`?) — worth adding if subtree rebuilds ever
  feel sluggish; measure first.

## Pointers for whoever picks this up

- Protocol verbs and semantics: `protocol/session.go` (new/set/destroy/
  sub/unsub/describe/template/alias; correlation keys; D-numbered rules in
  comments).
- Introspection shapes: `protocol/describe.go`; client entry:
  `client/client.go: Conn.Describe`.
- Registration pattern (one `*_protocol.go` per trinket; typed prop
  helpers): `objects/trinkets/protocol_reg.go` and any sibling, e.g.
  `objects/trinkets/treeview_protocol.go`.
- A complete UI-as-script to study first: `examples/demoapp/scripts.go`.
- Naming/type-system conventions the builder must respect:
  `docs/property-vocabulary.md` (D17 typing, D18 case namespaces).
