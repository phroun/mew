# mew for Prose Writers — Ideas & Roadmap

WordStar still has a devoted following among working novelists (George R.R.
Martin famously drafts in WordStar 4.0; Robert J. Sawyer's essay "WordStar: A
Writer's Word Processor" is the canonical statement of why). Those users are
increasingly stuck maintaining DOSBox rigs because nothing modern respects
their muscle memory. mew's succession pitch: a static Go binary that runs in
any terminal for the next thirty years, speaks WordStar's key language
natively, and quietly exceeds it where a modern engine allows.

This document collects what the current (programmer-leaning) feature set is
missing from the author/novelist standpoint, tiered by how hard each gap
blocks the pitch.

## Tier 1 — Blockers (an author can't live here yet)

### 1. Word wrap
The `wordWrap` config option exists but rendering is not implemented (long on
the deferred docket). For prose this is *the* feature: an author's paragraph
is one logical line, and today it scrolls off the right edge.

- Soft wrap at the window edge first (display-only; file unchanged).
- Then the classic WordStar hard-wrap workflow as an option: a right margin,
  wrap-as-you-type, and paragraph reflow (WordStar's `^B`) for people who
  want real line breaks in their files.
- Performance note: multi-kilobyte paragraph lines are exactly where careless
  per-line work hurts; the O(n²) line-access fix (GetLineRange) was a
  prerequisite. A visible-line map (docket item) is the companion piece.

### 2. Word count
Authors think in words the way programmers think in lines.

- Live word count in the modebar (the context slot can show it for prose
  buffers the way it shows the function breadcrumb for code).
- Word count of the marked block.
- Session delta — "words written today." Garland's revision history could
  make this honest (count against the session-start revision) rather than
  approximate. Writers on deadlines/NaNoWriMo genuinely love this.

### 3. Spell check
Non-negotiable for this audience.

- Hunspell-format dictionaries are ubiquitous and well-documented.
- Misspelling paint rides the existing SGR-per-rune renderer machinery (same
  channel the syntax highlighter feeds).
- Personal/project dictionary drops naturally into the `.mew` directory
  system (per-novel character names, invented places).
- Suggestions UI can wait; underline-and-ignore is the MVP.

### 4. Autosave & fearless backups
Writers' single deepest anxiety is losing work.

- Timed autosave; rotating backups into per-project `[storage]`.
- Crash recovery on next launch.
- Longer term: expose garland forks/revisions as *named drafts* ("the
  version where the detective was the killer") — an undo-forever story no
  mainstream writing tool has.

## Tier 2 — WordStar parity for the faithful

- **Place markers 0–9**: `^K0`–`^K9` set, `^Q0`–`^Q9` jump. Trivial on the
  named-mark system; persisting them across sessions via cold storage would
  *exceed* the original.
- **Sentence & paragraph motions/deletes**: we move by char/word/line/page;
  prose people move by sentence and paragraph.
- **An authentic `mappings=wordstar` profile**: the named-mappings system
  already exists, and the active-sequence display with completions is
  WordStar's two-stroke menu prompts reborn. Ship the full `^Q` quick menu
  and `^K` block menu so a WordStar hand is at home in minute one.
- **Find/replace options**: whole-word, backward, ask-each/global — and
  case-preserving replace (cat→dog also fixes Cat→Dog), which prose demands
  far more often than code does.

## Tier 3 — Where mew could exceed WordStar

- **Typographer mode**: curly quotes, em-dashes (`--` → —), ellipses as you
  type; style-aware (US curly vs. guillemets vs. German lows). We already
  *match* these pairs in go_match and the Option layer already *types* them;
  auto-substitution closes the loop.
- **A prose "grammar"**: the jsf engine is a general context machine —
  dialogue (text inside quotes) is just "string context" wearing a different
  hat. Possibilities:
  - Dialogue in its own subtle color; "words of dialogue" statistics.
  - Inline author notes (`[[like this]]`) as "comments": excluded from word
    count, stripped on export.
  - Opt-in editing-pass mode flagging filler words/adverbs.
- **Chapter/scene navigation**: the outline breadcrumb already understands
  markdown headings; add next/prev-section motions and scene-separator
  (`***`) awareness, plus a jump-to-heading picker, and a 120k-word
  single-file novel becomes navigable.
- **Manuscript concerns**:
  - Page-count estimate (words ÷ 250) in stats.
  - Clean export path (markdown → pandoc etc.); standard manuscript format
    eventually.
  - At minimum *tolerance* for WordStar dot commands (`.pa`, `.he`): a
    grammar that dims them instead of choking, so 1985 files open
    respectfully.
- **Typewriter scrolling / focus mode**: keep the caret line vertically
  centered; optionally dim everything but the current paragraph. Terminal
  editors are already the best distraction-free environment — lean in.

## Already in place (quietly serving this audience)

- WordStar-flavored default bindings; two-stroke sequences with on-screen
  completion prompts.
- Prose-pair matching (curly quotes, guillemets, CJK brackets) in go_match.
- The macOS-Option typographic layer on every platform (—, …, æ, «, ≠).
- RTL/bidi support — real multilingual prose.
- Per-project `.mew` config (per-novel settings, storage, dictionaries).
- PawScript macros — writers' rituals automated.
- Large-file performance (O(n²) line access fixed 2026-07).

## Suggested "author MVP" sequence

1. Word wrap (soft wrap first)
2. Word count in the modebar
3. `mappings=wordstar` authentic profile
4. Autosave + rotating backups
5. Spell check (first big post-MVP feature)

Word wrap is the load-bearing item; everything else composes around it.
