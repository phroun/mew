# G1 Metrics Audit — DefaultCellMetrics() call-site classification

Audited 2026-07-05 on branch `claude/project-survey-mr6k4v`. Companion to
the G1 section of `graphical-mode-plan.md`. This is the refactor
checklist: every call site of `core.DefaultCellMetrics()` outside its
definition, classified by what question the code is actually asking.

## The two-concept model (context)

- **Grid metrics** — "how big is a virtual row/column *in this
  container*?" A deliberate layout vocabulary: each container may define
  its own row/column size in native units (for placement detail and
  density), inherited from its parent container, rooted at the display
  service's default (derived from the system default font size).
  Overridable per window/container in all modes, including TUI.
- **Text metrics** — "how much space does *this text* in *this font*
  occupy on *this render target*?" Target-dependent; answered by the
  backend / text engine, never by grid arithmetic.

In TUI mode with default settings the two coincide numerically, which is
why the code never had to distinguish them. They diverge in graphical
mode and under non-uniform fonts (Tuesday already demonstrates this).

## Categories

- **A** — grid question, `*core.Painter` in scope → trivial swap to
  `p.Metrics()`.
- **B** — grid question, no painter in scope (SizeHint, hit-testing,
  geometry) → needs container-inherited metrics via parent walk.
- **C** — text question in disguise (width/char-count/truncation) →
  route to text measurement (font.MeasureText now; TextMeasurer later).
- **D** — plausibly intentional canonical default → human-reviewed;
  resolution below.
- **E** — cell-grid content (the trinket IS a grid) → metrics from the
  trinket's own font/container.

## Totals

| Category | Count |
|---|---|
| A | 1 |
| B | 102 |
| C | 14 |
| D | 10 |
| E | 6 |
| **Total** | **133** |

## Structural findings

1. **No container metrics storage exists yet.** No `CellMetrics` field
   or accessor on Desktop, Window, or MDIPane; the only stored metrics
   are `TUIBackend.metrics`, surfaced via `Painter.Metrics()` during
   paint. **Fonts already have the needed pattern**: `FontProvider` +
   `core.FindEffectiveFont` parent walk (`core/font.go:245-282`) with
   `Font()/SetFont()/EffectiveFont()` on Desktop, Window, MDIPane. G1
   builds the metrics analogue: a `CellMetricsProvider` (name TBD) +
   `FindEffectiveCellMetrics` walk, rooted at the Desktop's stored
   default. The 102 B-sites then rewrite mechanically onto it.
2. **One true dead-end:** `trinkets/spacer.go:19 NewSpacer()` bakes a
   cell-based size in the constructor, before the trinket has a parent.
   Needs lazy sizing at first layout rather than a parent walk.
3. **Paint paths are already clean.** Paint methods use `p.Metrics()`
   (desktop.go:1621, window.go:794, mdipane.go:1083); only one paint
   method calls the global constructor (the single A-site). The
   contamination lives in the non-paint paths that had no metrics
   source to ask — i.e., the missing inheritance mechanism.
4. **Free-floating popups** (Menu/MenuBar overlays) may have a nil
   `Parent()`; the parent walk needs the same fallback treatment
   fonts use.

## D-site resolution (the "intentional?" question)

- `backend/tui.go:97` (`DefaultTUIOptions`), `:113` (`NewTUIBackend`
  zero-value fallback) — **keep**: the backend is the legitimate source
  of its own default.
- `trinkets/desktop.go:1228, 1428, 1491, 1522, 1530, 1539, 1567, 1736`
  (`ChildAt`, `layoutChildren`, `ClientArea`, `MenuBarHeight`,
  `StatusBarHeight`, `StatusBarBounds`, `DockBounds`,
  `HandleMousePress`) — Desktop is the root container, so "which
  container's metrics?" is "its own." **Resolution: Desktop gets a
  stored metrics field** (seeded from backend/system settings — the
  display service's default, per the decided model) and these eight
  read it. Functionally identical today; that field becomes the root
  of the inheritance chain for every B-site.

## Category C — text questions in disguise (14)

All but two sit next to `font.MeasureText`/`MeasureRunes` already; the
fix is local. The two starred sites use pure `charCount × CellWidth`
with no font at all — the clearest latent bugs (both misrender under
Tuesday today):

| Site | Method | Note |
|---|---|---|
| textinput.go:295 | `ensureCursorVisible` | CellWidth as cursor/space advance amid MeasureText math |
| combobox.go:651 | `SizeHint` | measures items via font |
| label.go:71 | `SizeHint` | TextHeight from line count; width via font |
| separator.go:76 | `SizeHint` | title via font |
| button.go:199 | `SizeHint` | text via font + cell chrome |
| checkbox.go:138 | `SizeHint` | text via font + 3-cell indicator |
| radiobutton.go:84 | `SizeHint` | text via font + 3-cell indicator |
| tabtrinket.go:511 ★ | `calculateTabBarWidth` | `(len(text)+4) × CellWidth`, no font |
| tabtrinket.go:526 | `calculateTotalTabsWidth` | text via font + separator chrome |
| dialog.go:179 | `MessageBox.calculateSize` | width from `len(line)` |
| menu.go:569 | `Menu.calculateSize` | item text via font + gutter/arrow |
| menu.go:1357 ★ | `MenuBar.dateTimeWidth` | `18 × CellWidth` clock width, no font |
| menu.go:1773 | `MenuBar.SizeHint` | sums title widths via font |
| menu.go:1790 | `menuTitleWidth` | title via font + spacing |

Related (not a DefaultCellMetrics site): `label.go wrapText` counted
every rune as width 1 — reproduced live via the word-wrap test row on
the demo's Selection tab. **Fixed 2026-07-05**: wrapText now measures
via the effective font and breaks at word boundaries (character
fallback for overlong words), covered by `trinkets/label_test.go` —
the repo's first unit tests.

## Category E — PurfecTerm (6)

purfecterm.go: 115 (`SizeHint`), 134 (`updateTerminalSize`), 308, 393,
424, 451 (mouse handlers). The trinket is genuinely a cell grid; its
cell size should come from its own font/container metrics rather than
the global default.

## Category A (1)

- combobox.go:973 `paintScrollbar` → swap to `p.Metrics()`.

## Category B — grid questions needing the inheritance walk (102)

By file (line: enclosing method):

- **layout/box.go** — 132: `BoxLayout.Layout` (spacing rounding;
  receives `container` directly — easiest B-fix, no walk needed).
- **trinkets/mdipane.go** — 608 `TileWindows`, 654 `CascadeWindows`,
  804 `positionWindow`, 869 `detectResizeEdge`, 1195
  `HandleMousePress`, 1323/1387 `HandleMouseMove`.
- **trinkets/panel.go** — 100 `Layout` (border inset), 154 `SizeHint`
  (20×10 placeholder).
- **trinkets/textinput.go** — 342 `SizeHint` (fixed 20-char default).
- **trinkets/combobox.go** — 515 `registerPopupOverlay`, 923
  `scrollbarGeometry`, 999/1086/1275 popup mouse handlers,
  1526/1596/1696 mouse handlers.
- **trinkets/progress.go** — 173 `SizeHint` (cell block-bar sizing).
- **trinkets/button.go** — 458 `HandleMouseMove`.
- **trinkets/splitter.go** — 211 `dividerBounds`, 458 `HandleMouseMove`,
  573 `HandleKeyPress`.
- **trinkets/dock.go** — 118 `entriesPerRow`, 133 `RowCount`, 152
  `RequiredHeight`, 353 `HandleMousePress`.
- **trinkets/scrollarea.go** — 149 `ScrollBar.SizeHint`, 261/329
  ScrollBar mouse handlers, 585 `EnsureRectVisible`, 726
  `viewportBounds`, 748 `calculateScrollBarNeeds`, 803
  `updateScrollBars`, 829 `ScrollArea.SizeHint`, 994/1029/1065 mouse
  handlers.
- **trinkets/tabtrinket.go** — 455 `tabBarHeight`, 463 `contentBounds`,
  563 `scrollButtonWidth`, 606 `isLastTabFullyVisible`, 708
  `vertVisibleCount`, 756 `vertScrollbarGeometry`, 867 `SizeHint`,
  2390 `HandleMousePress`, 2468 `handleTabBarClick`, 2702
  `ensureTabFullyVisible`, 2831/2867/2896 `HandleMouseMove`.
- **trinkets/listview.go** — 205 `SetCurrentIndex`, 362 `ensureVisible`,
  374 `SizeHint`, 469 `scrollbarGeometry`, 604/615 `HandleKeyPress`,
  641 `visibleCount`, 663 `HandleMousePress`, 733 `HandleMouseMove`.
- **trinkets/treeview.go** — 187 `SetCurrentIndex`, 416
  `clampScrollOffset`, 447 `ensureVisible`, 459 `SizeHint`, 556
  `visibleCount`, 564 `scrollbarGeometry`, 721/732 `HandleKeyPress`,
  792 `HandleMousePress`, 897 `HandleMouseMove`.
- **trinkets/menu.go** — 234 `SetAvailableHeight`, 1033 `openSubMenu`,
  1106 `HandleMousePress`, 1178 `HandleMouseMove`, 1364
  `scrollButtonWidth`, 1390 `isLastMenuFullyVisible`, 1428
  `ensureMenuVisible`, 1488 `clampScrollOffset`, 1661 `OpenMenu`, 1756
  `calculateMenuX` (ellipsis part), 2225/2367/2444 mouse handlers.
- **trinkets/dialog.go** — 451 `FileDialog.setupUI`, 841
  `NewInputDialog`.
- **trinkets/desktop.go** — 1919 `StatusBar.SizeHint` (StatusBar is a
  child trinket, not the Desktop root).
- **trinkets/spacer.go** — 19 `NewSpacer` (the dead-end; lazy sizing).
- **window/window.go** — 640 `contentBounds`, 1216 `buttonAtPosition`,
  1340 `handleTitleBarKey`, 1659 `constrainBoundsForMovement`, 1862
  `HandleMousePress`, 2029 `SizeHint`.
- **window/manager.go** — 143 `detectResizeEdge`, 719 `MapToScreen`,
  785 `positionWindow`, 839 `TileWindows`, 883 `CascadeWindows`, 1016
  `HandleMousePress`, 1133/1209 `HandleMouseMove`.

## Denomination model — corrected semantics (2026-07-05)

An earlier note here misread the per-window override's double-spacing
as intended behavior. Corrected model (per D8's author):

**CellMetrics is a coordinate denomination, not a spacing knob.** It
defines the exchange rate between abstract units and rows/columns per
container — like DPI. Changing a container's metrics changes what the
numbers *mean*, never how big things *look*:

- Sizes expressed in rows/columns (`TextHeight(1)`, "one column of
  chrome") are denominated in the container's units and are **visually
  invariant** under re-denomination: one row is one row whether it is
  represented by 8, 16, or 32 units.
- Only **explicit numeric unit values** (SetSize/SetBounds literals,
  explicit hints) change interpretation: `Height: 64` is 2 rows in a
  32-unit window, 4 rows in a 16-unit one. Apps choose their own
  addressing resolution — that is the feature.

The double spacing observed via the demo toggle is therefore a
**denomination leak**: trinkets produce values in the window's currency
while the render path converts to screen cells at the backend's rate,
with no exchange at the border. Work required to close it:

1. **Boundary scaling in the paint/input path.** Entering a subtree
   whose metrics differ, the painter composes a scale transform (ratio
   of denominations per axis; `core.Transform` already carries unused
   `ScaleX/ScaleY` — built for this). Input events apply the inverse
   on descent; a rounding policy is defined once at the device edge.
2. **Text metrics are denominated too.** `Font.MeasureText`'s
   hardcoded 8 and `LineHeight`'s 16 are `DefaultCellMetrics` values
   in disguise: a Monday character is "one column" = the asking
   container's `CellWidth`. Measurement must answer in the caller's
   denomination; then text and grid scale together and no
   wrapped-vs-row inconsistency exists.
3. **Acceptance test:** the demo's "double-height grid" toggle must
   become a visual no-op for row-denominated content (the selection
   list), while an explicitly-sized element visibly changes. Today the
   toggle exposes the leak; that is its current documentation value.

**Status 2026-07-05 — boundary exchange implemented.**

- `Painter.WithDenomination(parent, child)` composes the scale
  transform; `Painter.WithTransform` composition order fixed (new
  transform applies first — immaterial for translations, essential for
  scales). `core.ExchangeX/Y/Size` convert values between
  denominations; `core.ParentCellMetrics` resolves a trinket's outer
  currency.
- Boundaries live where overrides can: **Window** content (layout,
  paint, ChildAt, mouse press/move/release, SizeHint) and **Panel**
  (layout, paint, ChildAt, mouse, SizeHint/MinimumSize/
  HeightForWidth). A container's bounds/chrome stay in the parent's
  currency; its interior is denominated by its override.
- A text line occupies one grid row in the container's denomination
  (Label/Checkbox/RadioButton HeightForWidth use `metrics.CellHeight`,
  not `font.LineHeight`).
- Invariance is tested: `TestDenominationInvariance` (interior hint
  changes, outer hint does not).

**Update (same day):** `MapToScreen` is now denomination-aware
(exchanges at every re-denominating container boundary, window content
included; scroll offsets use the scroller's own denomination), and
popups are formally **desktop-surface overlays**: `WindowManager`
exposes `ScreenCellMetrics()`, and ComboBox captures it at popup-open —
all popup-space geometry, painting, and input use the screen currency
while the in-window field stays interior. The splitter drift on
toggle-off was findings #2 + #3 conspiring (self-referential hint +
no-shrink stretch = a one-way ratchet); both fixed. End-to-end
round-trip invariance is tested against the demo hierarchy
(`TestWindowDenominationLayoutInvariance`).

Known residuals:

- Desktop paints its own children without boundary logic — correct
  while the desktop's root override equals `DefaultCellMetrics`; needs
  the same treatment if a backend ever reports different metrics.
  (Becomes a prerequisite when the graphical substrates land.)
- ~~MDIPane has no boundary machinery yet~~ — **done 2026-07-05**:
  same pattern as Panel/Window (`denominations()`/`toInterior`; one
  exchange at the boundary). `ClientArea` is interior currency, so
  positioning/tile/cascade/maximize inherit correctness; Paint runs
  on a `WithDenomination` painter; ChildAt and all three mouse
  handlers exchange at entry, so drag/resize state stays interior.
  Covered by `TestMDIPaneDenominationBoundary` (hit-testing agrees
  with paint position under an override; maximize fills the interior
  client area; toggle-off restores identity).
- FileDialog/InputDialog paint content manually on the window painter
  (outer space) with interior metrics — harmless until a dialog
  carries an override; normalize when dialogs are reworked.
- Hardware cursor placement for focused text inputs, if routed outside
  MapToScreen, may still need auditing under overrides.

## Adjacent layout/sizing-contract findings

Surfaced while building the Selection-tab wrap-test row (2026-07-05).
Not DefaultCellMetrics sites, but they shape the sizing contract that
G1 / the D2 API-shape phase must formalize — server-side layout cannot
rely on hand-tuning around these:

1. **`NewLayoutItem` defaults `Align` to `AlignLeft`** (layout.go:20)
   while the `Alignment` type documents `AlignFill` as the default
   (types.go:107). In a vertical box, AlignLeft silently forces item
   width to hint width — a zero-hint trinket becomes invisible while
   still consuming stretch space.
2. **`calculateStretch` never shrank an item below its hint** — extra
   space was distributed, but an oversized hint pushed siblings
   off-screen with no recourse. **Fixed 2026-07-05**: when
   over-committed, stretch items now compress proportionally (elastic
   in both directions); non-stretch items keep their hints.
3. **`Splitter.SizeHint()` returned `Bounds().Size()`** — zero before
   first layout, and a ratchet after: layouts could grow it but (with
   finding #2) never shrink it back — the cause of the splitter
   drifting on denomination toggle-off. **Fixed 2026-07-05**: modest
   fixed hint; splitters are meant to be stretched by their layout.
4. **Label has no height-for-width** — a wrapped label's true height
   depends on the width it is given, but `SizeHint()` is
   width-independent, so nothing can size a wrapped label correctly.
   **Fixed 2026-07-05 (D9)**: `core.HeightForWidther` interface;
   implemented by Label and opt-in wrapped Checkbox/RadioButton,
   consulted by BoxLayout, propagated by Panel. DockRow deliberately
   not migrated (D9); MessageBox pending. Tests in
   `trinkets/label_test.go`. Note the new HFW code paths add a few
   `DefaultCellMetrics()` call sites (checkbox/radiobutton
   HeightForWidth, Panel border inset, BoxLayout.HeightForWidth) —
   deliberate parity with their existing siblings; the G1 sweep
   collects them all together.
5. **Font-dependent SizeHints reshape the whole layout on font change**
   — sometimes wanted (buttons fitting text), sometimes not (a fixed
   design grid); there is currently no way for a trinket to declare
   whether its size derives from text metrics or grid metrics. This is
   the D8 two-concept distinction surfacing at the API level.
6. **`wrapText` was character wrap, not word wrap** — no word-boundary
   logic, and it counted runes instead of measuring. **Fixed
   2026-07-05**: breaks at word boundaries and measures candidate lines
   via the font (character fallback for overlong words); tests in
   `trinkets/label_test.go`. Will re-route from `font.MeasureText` to
   the TextMeasurer when G1 lands.
7. **`Panel.SetBorder(true)` with the zero-value `BorderStyle` drew an
   invisible border** (NUL runes). **Fixed 2026-07-05** (during the
   protocol trinket-binding work): enabling the border defaults the
   style to single lines when none was set.

## Suggested G1 execution order

1. ✅ **Done 2026-07-05** — inheritance mechanism built, mirroring the
   font machinery: `core.CellMetricsProvider` +
   `core.FindEffectiveCellMetrics` parent walk; `TrinketBase` stores an
   optional override with `SetCellMetrics` / `CellMetricsOverride` /
   `EffectiveCellMetrics` (so Desktop, Window, MDIPane, and every
   trinket get override capability by embedding); Desktop seeds its
   override from `backend.Metrics()` in `SetBackend`, rooting the
   chain. Layouts are not trinkets: BoxLayout resolves metrics from its
   container (or a `SetMetricsSource` trinket wired by Panel).
   Inheritance/override/clear covered by
   `TestEffectiveCellMetricsInheritance`.
   **Pilot conversions:** layout/box.go (Layout + HeightForWidth),
   trinkets/panel.go (all 3 sites), label.go, checkbox.go,
   radiobutton.go — all now use `EffectiveCellMetrics()`.
2. ✅ **Done 2026-07-05** — desktop.go's chrome sites (including
   StatusBar) read effective metrics rooted at the stored override;
   tui.go keeps its 2 as the root source.
3. ✅ **Done 2026-07-05** — trinkets/ B-sites swept onto
   `EffectiveCellMetrics()` (including paint-time `p.Metrics()`
   layout math, which is interior-currency). Deliberately NOT
   converted: window/window.go and window/manager.go sites (window
   chrome and manager geometry are outer/desktop currency — equal to
   the default today; route via the desktop's stored metrics when the
   desktop boundary is generalized) and spacer.go's constructor
   dead-end (needs lazy sizing).
4. ✅ **Resolved 2026-07-05** — C-sites closed out after live
   verification under Tuesday (user-tested):
   - `label.go wrapText` — fixed earlier (font-measured word wrap).
   - `tabtrinket.go calculateTabBarWidth` (★ withdrawn) — the vertical
     tab bar's width is a grid-design choice (N columns from longest
     label's rune count); tab text paints with the font and truncates
     gracefully. Verified correct-looking under Tuesday; reclassified
     as grid, not text.
   - `menu.go dateTimeWidth` (★ withdrawn) — the clock is cell-rendered
     chrome (`DrawCell` per rune, no font), so cell-count measurement
     matches its rendering exactly. Internally consistent; whether the
     clock should be font-aware is a style decision, deferred.
   - `dialog.go MessageBox.calculateSize` — message text is ALSO
     cell-rendered (`DrawCell` per rune); `len(line)` sizing matches.
     Internally consistent; becomes font-aware naturally when
     MessageBox adopts the wrapped-Label approach (D9 future adopter).
   - `textinput.go ensureCursorVisible` — correct as written:
     character advances via the font, end-of-text cursor is one grid
     cell (a block cursor is a grid fact).
   - `backend/tui.go DrawBox` title truncation — a cell-level
     primitive drawing runes 1:1; rune-count truncation matches.
   - `NewSpacer` dead-end fixed: default size resolves lazily against
     effective metrics at layout time instead of baking the global
     default in the constructor.
5. ✅ PurfecTerm E-sites now use its effective (container) metrics —
   its cell grid follows the container's denomination.
6. ✅ Verified live (2026-07-05): default rendering unchanged; the
   Selection-tab denomination toggle is visually invariant round-trip,
   including splitter position and popups.

**G1 is complete.** Remaining items are tracked residuals (below), not
phase blockers: MDIPane boundary machinery, desktop-children boundary
if a backend reports non-default root metrics, manual-painting
dialogs, window/manager outer-currency routing formalization, and the
optional font-awareness of cell-rendered chrome (clock, MessageBox
text).
