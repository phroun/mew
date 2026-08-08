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
| `raw`   | M1   | the raw byte stream, verbatim               | live, as bytes arrive    | mew-only (tap the output pump)          |
| `final` | M2   | final scrollback + used screen              | once, at session death   | mew-only — **implemented**              |
| `lines` | M3   | ordered transcript, each line as it scrolls off | live                | needs a purfecterm scroll-off callback  |
| `live`  | M4   | live screen mirror                          | live, on every change    | needs a purfecterm event stream         |

`raw`/`final` sit above the purfecterm module boundary (mew-only). `lines`/`live`
require new upstream events and carry the same upstream cost as any purfecterm
change.

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

- Implemented: the `off` / `final` rungs, tri-state resolution, and the
  `--plain` / `--text` formats (full / plain / text) via `SaveScrollback*Opts`
  and the ephemeral-cursor fold.
- Reserved but not implemented: rungs `raw`, `lines`, `live` (parsing them is a
  clear "not implemented yet" error, so the vocabulary is stable).
