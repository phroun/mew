# Considering: HTTP browsing in mew

> **Status: speculation, not a plan.** This is a design conversation recorded
> for later, with rough effort guesses attached. Nothing here is committed to,
> scheduled, or prototyped. Estimates are calendar time for one focused
> developer and should be read as order-of-magnitude, not as a bid.

## The idea

Google recently stopped supporting Lynx and elinks, on the grounds that they
cannot run JavaScript. mew already has most of the parts a replacement would
need: a garland-backed wiki engine, a link/browse layer, and a display
substitution pipeline. Add an MIT-licensed QuickJS, parse HTML into a DOM held
as a reference tree, project the visible nodes into browsable markdown, and let
JS mutate the tree with changed nodes flowing back into garland.

Deliberately **not** a general browser. Read-only pages. Its own URL scheme, so
undesirable functionality can be switched off at the boundary. Most CSS styling
possibilities ignored; mew's own font rules win. Target the sites a
developer-type actually wants in a terminal — Wikipedia, basic Google search,
GitHub — and explicitly not applications like Notion or Canva.

## Why this architecture fits unusually well

- **Garland is already most of a DOM.** A persistent tree with revisions, marks
  that survive edits, and undo is exactly what a DOM reference tree wants, and
  exactly what people hand-roll badly.
- **The display-substitution layer is already a semantic-tree renderer.**
  Buttons, headings, double-width, link spans, key badges: mew already turns a
  marked-up tree into browsable cells.
- **The overlay config system is a plausible cascade skeleton** — the
  `[options]` / class / grammar / type resolution order is the shape a CSS-ish
  cascade would want, if one ever proves necessary.
- **QuickJS in Go is well-trodden.** Bindings exist, the licence is compatible,
  and a DOM shim over an existing tree is mechanical work rather than research.

## Read-only changes the risk profile

The original worry was round-tripping edits: a DOM node's identity has to
survive both JS mutation and re-render, or marks and undo drift — and JS plus
the user are two writers to one tree. That is the same class of bug as a paint
and a hit test disagreeing about a coordinate frame, and it is worth avoiding
structurally rather than defensively.

Read-only removes it. The DOM tree stays separate from the display buffer and
the flow is one-directional: DOM → generated markdown. The browsable text is
not itself editable.

## Scoping to real targets changes the estimate more

Wikipedia, basic Google search, and GitHub are substantially **server
rendered**. They mostly work in Lynx today. So for the core reading experience,
QuickJS may be far less load-bearing than it first appears — possibly not
needed at all for the first tier. Build the DOM→markdown pipeline first, measure
which targets are actually broken without JS, and only then decide whether the
engine earns its keep.

Rough guess at how the three land with no JS at all:

| Target | Expectation without JS |
|---|---|
| Wikipedia | Fine. Probably better than Lynx, given mew's heading/link/table rendering. |
| Google search | HTML is there; the fight is consent interstitials and redirect chains, not JS. |
| GitHub | Hardest of the three. Repo browsing and issues are reasonable; the **file viewer** is heavily client-side and may genuinely want JS — or a rule that rewrites to `raw.githubusercontent.com`. |

## Per-site profiles are the real lever

A small **declarative per-domain profile** buys more usable web per hour of
work than a JS engine does: URL rewrites, selectors to keep or drop, "prefer
the print/mobile/no-JS variant", cookie preseeds. This is how w3m and Lynx
users cope in practice, and it fits mew's existing overlay-config idiom
naturally. It should be a first-class part of the scheme design, not an
afterthought bolted on when a site misbehaves.

## Shadow layout: track metrics without rendering them

The one hard problem that survives the narrowing is that JS-dependent sites ask
about **layout**, not DOM: `offsetWidth`, `getBoundingClientRect`,
`scrollHeight`, `IntersectionObserver`. Frameworks branch on the answers, and
zeros make them render nothing or spin.

The good answer is to **compute a real box model on the DOM side and simply not
render it.** A synthetic viewport, block/inline flow, widths and heights and
offsets maintained as the tree mutates — genuine arithmetic that happens to
drive nothing visible. mew's cell rendering stays a separate, much simpler
projection of the same tree.

Two things recommend this over faking values:

1. **Self-consistency comes for free.** Ad-hoc lies contradict each other the
   moment a script compares two measurements or sums children against a
   parent. A real box model cannot contradict itself.
2. **It decouples the two problems.** The layout engine answers to the DOM and
   to CSS-as-metrics; the markdown projection answers to mew's font rules and
   cell grid. Neither has to compromise for the other, and the shadow layout
   can be as crude or as faithful as a target site demands without touching how
   anything looks on screen.

It also means CSS can be honoured *as measurement* while still being ignored
*as appearance* — `width`, `display`, `position` inform the box model; colours,
fonts and decoration are discarded in favour of mew's own rules. That is a
clean split, and it is the reason "ignore most CSS" is a simplification rather
than a compromise.

Cheap mitigation while the box model is rudimentary: report a large non-zero
viewport and fire observers immediately on register. Crude, but it satisfies
most reveal-when-visible patterns for nothing.

## Remaining risk: the platform surface

Not hard individually, long-tailed collectively, and sites fail loudly when any
piece is missing: `fetch`, XHR, cookies, redirects, `localStorage`, timers, the
microtask queue, `MutationObserver`, `history.pushState`. Scoping to a few
targets bounds this, but it does not eliminate it.

## Security

Running untrusted JS with network access inside an editor that has the user's
filesystem deserves care. QuickJS is a reasonable isolate; the shims are where
escapes live. Fetches should go through a brokered, permission-scoped seam
rather than raw `net/http` from inside the editor — the same instinct that made
`PTYProvider` and the host `FileSystem` host seams rather than direct calls.

## Suggested sequencing

1. **Tier 1 — no JS. 1–3 weeks.** Fetch → parse → DOM → markdown. Links,
   tables, headings, lists. CSS read only for `display:none` / `visibility` and
   semantic hints. Per-site profiles from the start. A `browse:` scheme in the
   wiki engine. Useful on its own merits, and it tests whether the
   DOM↔garland mapping is pleasant before betting on the hard part.
2. **Tier 1.5 — shadow layout.** A block/inline box model over the DOM,
   unrendered. Independently testable: assert box arithmetic, never pixels.
3. **Tier 2 — QuickJS. 3–6 weeks, behind a build tag.** DOM shim, timers,
   fetch, mutation → re-render changed subtrees. Optional rather than
   load-bearing if tier 1 covers the targets.

Do not build a CSS cascade until a target site forces it.
