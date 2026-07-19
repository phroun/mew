# The `editor` trinket — contract & placeholder

The `editor` trinket is KittyTK's **monospaced, multiline** text-editing widget —
the `<textarea>`-and-up counterpart to `TextInput` (which owns single-line,
proportional input). Apps target **one** `editor` contract; the *implementation*
is chosen at build time:

- **Vanilla KittyTK** ships a **placeholder** editor (the subject of the second
  half of this doc): functional-but-lame, so an app that needs a text editor can
  still operate on a stock build.
- **mew** ships a modified KittyTK where `editor` is the full mew editor — the
  same contract, richly honored.

Because both honor the same contract, an app runs on either without changes
(cf. Chrome's built-in PDF viewer vs. a browser that just hands the file to an
external reader).

## Design principles

1. **The contract is bounded by the lame implementation.** Anything the contract
   *requires* must be honorable by the placeholder. That's why there is no
   baseline per-keystroke `change` event — the placeholder shells out to an
   external editor and physically cannot emit one.
2. **Editor policy defaults to "let the editor decide."** `wrap`, `tab_size`,
   `syntax`, `line_numbers` default to *inherit*, not to a literal. The app
   overrides only with clear reason; otherwise the editor's own resolution wins
   (in mew, that means the user's `editor.conf` and grammar overlays still govern
   inside an embedded widget).
3. **Files live below the contract.** The host brokers a permission-scoped
   virtual filesystem (and a pre-scoped `Glob`), hands out `filename` handles,
   and performs local disk I/O directly — off the wire — when the file is local,
   notifying the app on completion. The trinket surface is about *editing*, not
   files.

## Properties (app → editor)

**Core** — the placeholder honors these meaningfully:

| name | kind | default | meaning |
|---|---|---|---|
| `filename` | string | `""` | Host-granted handle; the editor opens/saves/locks it through the brokered FS. Wins over `value` when both are set. Placeholder: the file it opens in an external editor. |
| `value` | string | `""` | Ephemeral, non-file-backed text: seed content, read back via `commit`. |
| `placeholder` | string | `""` | Hint shown when empty. |
| `readonly` | bool | `false` | View only; disables the edit affordance. |
| `caption` | string | `""` | A title/label on the frame. |

**Rich** — mew honors; the placeholder accepts and ignores. Default is
*inherit* (`"default"`), so the editor's own resolution governs unless overridden:

| name | kind | default | meaning |
|---|---|---|---|
| `wrap` | bool | `default` | Soft-wrap long lines. |
| `tab_size` | int | `default` | Tab width. |
| `syntax` | string | `default` | Grammar id; `"auto"` = detect from `filename`; `""` = none. |
| `line_numbers` | bool | `default` | Show line numbers. |
| `caret` | string | `""` | Place cursor / scroll-to, `"line:col"` (1-based). Same mechanism as mew's `+N[:col]` launch argument. Read back via the `caret` event. |

## Events (editor → app)

| event | fields | when |
|---|---|---|
| `commit` | `filename` *or* `value` | User finished/accepted. File-backed: after a successful save through the FS (`filename`). Ephemeral: on accept (`value`). |
| `saved` | `filename` | An incremental save completed through the FS while editing continues (the "notify on completion" checkpoint). |
| `dirty` | `dirty` (bool) | Modified-state toggled. |
| `cancel` | — | Discarded/closed without saving. |
| `focus` / `blur` | — | Focus gained/lost. |
| `caret` *(opt-in)* | `line`, `col` | Cursor moved. Chatty; the placeholder never emits it. |

There is deliberately **no baseline `change`** (principle 1).

## Imperative commands (deferred)

One-shot app→editor verbs — `save-now`, `reload`, `select-all`, `focus` — are
parked pending KittyTK's app→trinket command mechanism. When that shape is
decided, they attach here.

---

# Placeholder — goals

**Purpose.** A functional-but-lame `editor` for vanilla KittyTK, so apps that
need a text editor still operate on a stock build, and keep it honoring the
contract as it evolves so apps "feel like they have a line editor available."
It is intentionally minimal; the real experience is mew's job.

**Tier 0 — inline stub.**
- Renders a clearly-marked placeholder frame showing `caption` / `placeholder`
  and a short preview of the current text (or `filename`).
- Holds `value`; honors `readonly`, `caption`, `placeholder`.
- Accepts (and silently ignores) every rich property, so apps set them uniformly
  without error.

**Tier 1 — click-to-edit.**
- The frame is actionable: activating it writes the current text to a temp file,
  spawns the user's external editor (`$VISUAL` / `$EDITOR`) on it as an OS
  process, and on exit slurps the file back, updates `value`, and emits `commit`
  (plus `dirty` / `saved` as appropriate).
- Uses `filename` as the working file when the host granted one; otherwise a
  temp file seeded from `value`.
- **Non-goals:** real inline editing, live `change`, syntax highlighting, cursor
  reporting. Those are mew's.

**Coexistence / build tags.**
- The placeholder registers `editor` under `//go:build !mew`; the reference
  mew-backed editor registers `editor` under `//go:build mew`. They are mutually
  exclusive, so exactly one `editor` exists in any build.
- The mew example trinket (`editor_mew.go`, `editor_protocol_mew.go`) is
  **preserved** for reference while the placeholder matures. Long-term, the real
  editor moves to mew's side (mew ships a modified KittyTK) and the placeholder
  becomes the unconditional vanilla `editor`.

**Upstream.** This trinket and doc are MIT, intended for submission to
`phroun/kittytk`.
