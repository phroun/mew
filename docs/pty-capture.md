# PTY capture — argument surface

How a hosted PTY session's output is folded into its mew buffer, and how that is
spelled on the `exec` / `shell` command line. See also `internal/editor/pty.go`
(the capture path) and `internal/editor/execargs.go` (the parse).

## `--capture=<rung>` — the fidelity ladder

The rung chooses *when* and *how much* of a session's output is captured. It is a
single ordered axis, from cheapest/least to richest/most:

| Value   | Rung | Captures                                   | When                     | Cost / boundary                         |
|---------|------|--------------------------------------------|--------------------------|-----------------------------------------|
| `off`   | —    | nothing                                     | —                        | explicit disable (overrides a default)  |
| `raw`   | M1   | the raw byte stream, verbatim               | live, as bytes arrive    | **implemented** (v0.2.34 tee)           |
| `final` | M2   | final scrollback + used screen              | once, at session death   | **implemented**                         |
| `lines` | M3   | ordered transcript, each line as it scrolls off | live                | **implemented** (v0.2.35 OnLineOff)     |
| `live`  | M4   | live in-place screen mirror                 | live, on every change    | **implemented** (v0.2.36 event stream)  |

`raw`/`final` sit above the purfecterm module boundary (mew-only). `lines`/`live`
ride purfecterm's `CaptureObserver` seam: `raw` is `OnOutput` (v0.2.34), `lines`
is `OnLineOff` (v0.2.35); `live` adds the structural screen events (v0.2.36:
`OnWrite`/`OnCursorMove`/`OnNewline`/`OnLineWrap`/`OnBackspace`/`OnScrollLineOff`/
`OnClearScreen`).

`lines` is the resolved, ordered transcript — each line as the emulator
finalized it when it left the screen, captured live and so **unbounded by
purfecterm's scrollback cap**. A line rewritten in place before it scrolls off
contributes once; a wrapped logical line contributes one transcript line per
screen row (the on-screen view — `raw` preserves the original breaks); the
final on-screen tail is flushed at session end.

## Resolution is tri-state

The parsed capture value is `optional<rung>`, not a plain on/off:

- **unspecified** → inherit the configured default (none today, so it resolves to
  `off`; the seam is `Editor.resolveCapture`).
- **`off`** → explicitly disabled; beats a default.
- **`raw` | `final` | `lines` | `live`** → an explicit rung.

Keeping "unset" distinct from "off" is what lets a future `[options] capture=…`
default be overridden per-invocation with `--capture=off`.

A bare `--capture` (no value) means `final` — the flagship rung.

## Format — `--plain` / `--text`

The default keeps everything; you opt *down*. Format is meaningful only alongside
a capturing rung, and the two flags are mutually exclusive.

| Flag      | Meaning                                          | Realization                                                                 |
|-----------|--------------------------------------------------|-----------------------------------------------------------------------------|
| *(none)*  | full fidelity — keep all escapes                 | `final`: `SaveScrollbackANS`; `raw`/`live`: raw bytes                        |
| `--plain` | drop visual styling (SGR), keep positioning/layout | mew-side filter: strip `ESC[…m` (SGR + purfecterm's BGP/flip — all CSI-`m`). Uniform across rungs. |
| `--text`  | strip **all** VT escapes → pure text             | `final`: `SaveScrollbackText` (cell-walk, structurally exact); `raw`/`live`: mew-side strip-all filter |

Where a rung has no non-SGR escapes to preserve, `--plain` collapses to `--text`.
For `final`, `--text` uses purfecterm's cell serializer (never emits an escape by
construction) rather than a regex strip.

## Settled elsewhere

- **Trim** (drop the blank tail of the fixed-height screen grid) is always on for
  captures — not a user knob. `ScrollbackSaveOptions{TrimTrailingBlankLines}`,
  purfecterm ≥ v0.2.33.
- **Destination** is the editor buffer, folded at an **ephemeral cursor pinned to
  the caret the session was launched from** (`final` today; the streaming rungs
  reuse the same cursor to insert live).

## Parked as future

- A **config default** capture mode (the reason `off` exists as an explicit
  value).
- **File destinations** — owned by the separate redirect family (`--out` /
  `--outlog`), sharing the same tap; not part of `--capture`.
- **Named profiles** — a preset name expanding to a rung + format (+ destination)
  bundle.

## Status

- Implemented: the `off` / `raw` / `final` / `lines` / `live` rungs, tri-state
  resolution, and the `--plain` / `--text` formats. `final` folds via
  `SaveScrollback*Opts` at death; `raw` and `lines` stream live through the
  `CaptureSink` seam into the ephemeral cursor (`raw` = the byte tee, `lines` =
  serialized transcript lines), plus the runtime commands `viewport_pty_hide` /
  `_show` / `_toggle` / `_kill` and the `--hidden` flag.
- `live` (M4) folds a two-dimensional, in-place mirror of the screen through the
  same `CaptureSink` seam, driven by purfecterm's structural events (v0.2.36).
  Three ephemeral cursors bound the region — "start of terminal" (row 0),
  "terminal caret" (the write point), and "end of terminal" (the frontier). The
  region grows a row per newline until the screen is full, then a scroll
  advances "start of terminal" so the departed row stays above as history (O(1),
  nothing deleted). A newline steps past the current line's EOL rather than
  inserting one, so a row's remainder is never pushed down; overtypes replace
  cells in place; short lines pad with spaces. Colour (full format) rides inline
  as absolute `0;`-prefixed SGRs with a per-row start-pen array, so a
  single-colour run emits one SGR and nothing after. The engine stores no screen
  height and simply follows the event stream, which makes it correct across a
  resize.
  - Follow-up: the begin/end colour correction for a caret that jumps back into
    already-coloured cells (emit the landing pen, restore the prior pen after the
    overtyped chunk, replace rather than stack an SGR it lands on). Until then,
    colour is exact for streaming and sequential output; a mid-line recolour
    after a jump can leave a stale pen. `--plain` / `--text` are unaffected
    (no SGR in the document).
