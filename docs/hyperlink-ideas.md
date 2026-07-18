# Hyperlink System — Design Considerations (not yet built)

Goal: one link mechanism that works across three surfaces —

1. **Editable main buffers** (e.g. markdown/dokuwiki files being written),
2. **Read-only main buffers** (browsing docs/help as full documents),
3. **Work buffers** using **highlight-bar navigation** (no text caret; a
   selection bar moves between items) — the substrate for the navigable
   help system and the options/menu system.

A `focus_toggle` command (planned separately) switches focus between main
buffers and work buffers.

## 1. What a link *is* (representation)

Three providers, one interface. A link is a span (line, runeStart, runeEnd)
plus a target. Where the spans come from differs by surface:

- **Static tables** — generated work buffers (help pages, menus). The
  generator knows every link as it writes the content; it registers a link
  table alongside the buffer. No parsing, no decorations. Menus are then
  pure data: a list of (label, target, hotkey?).
- **Syntax-derived** — content buffers whose grammar already recognizes
  links (markdown.jsf has a `link` class; dokuwiki too). Derive spans from
  the existing syntax cache: zero storage, automatically in sync with
  edits, invalidated by the same ChangeSeq/watermark machinery. No new
  mechanism to keep honest.
- **Decoration-anchored** — for links that must survive arbitrary edits
  independent of any grammar: paired garland marks (span begin/end) plus a
  side table for targets. This is the same pattern as `_block_begin`/`_end`
  and fits the planned garland auto-mark feature. Probably *later*; the
  first two cover help/menus/markdown.

Suggested interface shape: a per-window `linkAt(line, rune)`,
`nextLink(from)`, `prevLink(from)`, `linksOnLine(line)` — with the provider
chosen by window kind. Memoize per ChangeSeq like the outline memo.

## 2. Targets: PawScript as the universal action

The unifying move: **a link target is a scheme-prefixed string, and the
dispatcher's escape hatch is a pawscript command.**

- `help:topic` — load a help topic into the current help window
- `goto:file#line` — open/focus a buffer at a position
- `mark:name` — jump to a named mark
- `url:https://…` — hand off to the terminal/OS (or just display)
- `cmd:set_option 'wordWrap','true'` — execute a command (menus!)

The options menu then falls out for free: each row is a `cmd:` link that
runs `set_option …` and regenerates the menu in place (preserving bar
position). Help cross-references are `help:` links. No bespoke menu engine.

**Security rule (important):** content-derived links (from files on disk —
markdown in a cloned repo, etc.) may only carry *navigation* schemes
(`help:`, `goto:`, `mark:`, `url:`). Only editor-generated static tables
may carry `cmd:`. Same philosophy as "profile.mew never runs from project
directories": opening a file must never be able to execute commands.

## 3. Two navigation modes (per-window)

Add a per-window nav mode: **caret** (today's behavior) vs **bar**.

- **Caret mode** (editable and read-only main buffers): links are inline.
  `link_follow` activates the link under the caret; `link_next`/`link_prev`
  jump between spans. In *read-only* buffers, Enter can BE `link_follow`
  (nothing to insert), which makes browsing feel like a pager/browser.
- **Bar mode** (work buffers): no caret. The bar is the focused *item*;
  up/down (and PgUp/PgDn) move between links in document order, Enter
  activates, Esc/q closes or returns focus. Two visual styles:
  - `line` — whole-line highlight (menus, lists; one item per line)
  - `span` — highlight just the link span (help pages with several links
    per line)

Rendering: reuse the existing `selectionRange` compositing path — the bar
is a one-line (or one-span) selection painted in a new scheme color
(`linkBar` / fall back to `selection`). Link spans themselves get a
`linkText` color + underline SGR so they're visible before focus reaches
them. Colors resolve through the normal `[colors.<class>]` cascade, so
help/menu windows can theme independently.

## 4. Focus & key routing

- Work buffers must become **focusable** (today only prompt windows take
  focus away from main buffers). The planned `focus_toggle` command plus
  FocusNext/Prev covers cycling; the modebar should indicate the focused
  window (it already knows classes).
- **Key routing in bar mode:** a focused bar-mode window should interpret
  keys as navigation, not insertion. Cleanest fit with the existing config
  cascade: per-window-class mapping layers (e.g. `[mappings.workbuffer]`,
  `[mappings.workbuffer.help]`) that override the global map while such a
  window has focus — mirrors how buffer-type color cascades already work.
  Fallback default: unmapped printable keys in bar mode do nothing (or do
  type-ahead item search later), never insert.
- **Hotkeys/accelerators:** the static link table can carry an optional
  hotkey per item (WordStar-style menu letters). Bar mode consults the
  focused window's table before the normal keymap.

## 5. Read-only buffers (new primitive)

Nothing today enforces read-only. Needed regardless of links:

- A window- or buffer-level `readOnly` flag checked at the mutation
  entry points (insert/delete/paste commands show "buffer is read-only"
  instead of editing). Buffer-level is safer (guards every path).
- Caret movement, search, block *copy*, and link following all still work.
- The help-as-documents story: builtin help topics can be embedded markdown
  opened read-only with the markdown grammar — links derived by syntax,
  rendered by the normal renderer. One renderer, one grammar, no bespoke
  help format.

## 6. Activation plumbing & history

- `link_follow` resolves the span → target → dispatcher. Dispatcher is a
  small scheme switch; `cmd:` goes through `PawScript.ExecuteAsync` (same
  deadlock-safety reasoning as the `cmd` command).
- **Back stack** (help browsing needs it): per-window history of
  (topic/generator, scroll offset, bar position). `link_back` pops.
  Regenerating menus after a `cmd:` activation should *preserve* bar
  position (same row) rather than resetting to the top.
- Menu regeneration: an activation that changes state (set_option) marks
  the window's generator dirty; the generator re-runs and the table is
  rebuilt. Generator = a Go func or a pawscript snippet — the latter would
  let users build their own menus.

## 7. Suggested build order

1. **Read-only flag** (small, independently useful).
2. **Focusable work buffers + focus_toggle + bar mode rendering** (reuse
   selection compositor; arrows/Enter/Esc routing).
3. **Static link tables + dispatcher** (help: / cmd: / goto:) → rebuild the
   options display as the first real bar-mode menu.
4. **Help system** on the same substrate (topics, cross-links, back stack).
5. **Syntax-derived links** in markdown/dokuwiki buffers (caret mode,
   link_follow/next/prev).
6. Later: decoration-anchored links.

The menu system (3) is the smallest end-to-end proof: one window class, one
static table, whole-line bar, `cmd:` targets.
