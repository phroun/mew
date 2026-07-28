# Feature request: OSC 52 clipboard integration for PurfecTerm

> **Implemented, as an applyable patch.** See
> [`upstream/0002-purfecterm-osc52-clipboard.patch`](upstream/0002-purfecterm-osc52-clipboard.patch),
> which applies to v0.2.27 **after**
> [`upstream/0001-purfecterm-csi-equals-prefix.patch`](upstream/0001-purfecterm-csi-equals-prefix.patch)
> (`git am 0001-*.patch 0002-*.patch`). It follows the design below with two
> deliberate departures, both noted inline: `ClipboardWrite` is a `*bool` so
> the zero `Options` keeps the safe default, and the parser gained a general
> `SetResponseSink` rather than a private reply path — DSR and DA are stubbed
> with *"would need to send response"* and want the same channel. The parser
> bug at the end ("A parser bug worth fixing in the same visit") is fixed in
> the same patch, because OSC 52 could not work without it. The GTK and Qt
> widgets are **not** wired — each needs its own few-line bridge from the
> callback to its platform clipboard.
>
> Verified against a clean v0.2.27: both patches apply, `go build`, and
> `go test ./ ./cli` pass, with an end-to-end run through `cli.Terminal` in
> embedded mode covering write, multi-selection, clear and query.

*From the mew/KittyTK integration work, July 2026. Written for the PurfecTerm
maintainers as a general-purpose feature request — the motivating case is a
guest editor inside a hosted terminal, but the design below is for every
PurfecTerm consumer: the embedded `cli` adapter, the standalone `cli` build,
and the GTK and Qt widgets alike.*

---

## What is being asked for

Support the standard clipboard escape sequence, **OSC 52**, in PurfecTerm's
parser, surfaced to each front end through a callback so every consumer can
wire it to its platform's real clipboard.

OSC 52 is the one channel a program running *inside* a terminal has to the
system clipboard. tmux, kitty, WezTerm, iTerm2, foot, alacritty, xterm and
modern Windows Terminal all speak it; vim, neovim, emacs, tmux, zellij and a
long tail of TUI programs emit it. A terminal emulator without it is a
terminal in which `vim "+y` quietly does nothing — the program believes it
copied, and nothing anywhere changed.

## The motivating case (why now)

mew hosts terminal sessions inside viewports (the `exec` command): a KittyTK
Editor trinket runs a child PurfecTerm per session, fed with the raw byte
stream from a real PTY. When the program in that session is *another mew* (or
vim, or anything), its own "copy to system clipboard" command has exactly one
possible transport: emit OSC 52 into its stdout, and trust the terminal to
act on it.

The receiving side is already fully wired: the hosted child PurfecTerm has a
restored parent chain to the desktop, and `findDesktop().SetClipboard(...)`
works today (it is what the context-menu Copy uses). The only missing link is
that the parser never surfaces OSC 52 — the sequence is consumed and dropped
in `executeOSC`, which today handles only the private 700x family and notes
`// Other OSC commands (title, etc.) could be added here`.

This is not mew-specific. Any consumer embedding PurfecTerm as a terminal —
the GTK widget in a GNOME app, the Qt widget, the standalone CLI — has users
running tmux and vim inside it, and all of them expect `set-clipboard` /
`clipboard: unnamedplus` to work.

## The sequence, precisely

```
OSC 52 ; Pc ; Pd  ST        (ESC ] 5 2 ; Pc ; Pd ESC \)
OSC 52 ; Pc ; Pd  BEL       (BEL-terminated form, equally common)
```

- **Pc** names the selection(s): any combination of the characters
  `c` (clipboard), `p` (primary), `q` (secondary), `s` (select), `0`–`7`
  (cut buffers). Empty Pc defaults to `s0` per xterm; in practice `c` is what
  programs send. Recommendation: honor `c` and `p`, ignore the rest.
- **Pd** is the payload:
  - **base64 data** → *write*: set the named selection(s) to the decoded
    bytes (UTF-8 text by convention).
  - **`?`** → *query*: the terminal replies on input with
    `OSC 52 ; Pc ; <base64 of current contents> ST`.
  - **anything that is not valid base64** (including empty) → *clear* the
    selection. This is xterm's rule and the safe way to handle garbage: a
    malformed payload clears rather than pastes noise.

## Proposed design

### Parse at the shared layer, act at the front end

The split that matches PurfecTerm's architecture: the **parser/buffer** layer
(shared by cli, GTK and Qt) recognizes OSC 52, decodes the base64, applies
the size cap and the policy — and then hands the *result* to a callback the
front end registered. Only the front end knows what "the clipboard" is on its
platform:

| Consumer | Write action | Read (query) source |
|---|---|---|
| GTK widget | `gtk_clipboard_set_text` (CLIPBOARD; and PRIMARY for `p`) | GTK clipboard |
| Qt widget | `QClipboard::setText` (Clipboard / Selection modes) | QClipboard |
| cli, embedded (KittyTK, mew hosting) | callback → host (`desktop.SetClipboard`) | host callback |
| cli, standalone in a real terminal | **re-emit the OSC 52 verbatim to the outer terminal** | pass through the outer terminal's reply |

The last row matters and is easy to forget: a standalone PurfecTerm running
inside kitty or tmux should *forward* OSC 52 outward rather than handle it,
exactly as tmux does (with `set-clipboard on`). That makes nested stacks work
— mew inside PurfecTerm inside tmux inside a desktop terminal, with one copy
command at the bottom landing on the real clipboard at the top.

### API sketch (in PurfecTerm's existing idiom)

Callback registration, following the `SetOnBell` pattern:

```go
// ClipboardEvent is one OSC 52 operation, already decoded and policy-checked.
type ClipboardEvent struct {
    Selections string // "c", "p", or both; already filtered to supported ones
    Data       []byte // decoded payload; nil for a CLEAR
    Query      bool   // true for a read request (Data is nil)
}

// SetOnClipboard registers the front end's clipboard bridge. For a write or
// clear, act on it. For a query, call reply (once, from any goroutine) with
// the current contents — or never, to deny; the parser answers the querying
// program only when reply is called.
func (t *Terminal) SetOnClipboard(fn func(ev ClipboardEvent, reply func([]byte)))
```

With no callback registered, OSC 52 is consumed and dropped — today's
behavior, and the correct default for a consumer that has not opted in.

Options, following the existing `Options` struct:

```go
type Options struct {
    // ...
    // ClipboardWrite enables acting on OSC 52 writes/clears. Default true:
    // a write is initiated by the user's own program and is what everyone
    // expects to work.
    ClipboardWrite bool
    // ClipboardRead enables answering OSC 52 queries. DEFAULT FALSE — see
    // Security. When false a query is answered with an empty payload
    // immediately (not silence: programs block waiting for the reply).
    ClipboardRead bool
    // ClipboardLimit caps one payload's decoded size. Default 1 MiB;
    // 0 = default. Oversized payloads are treated as a clear, not truncated
    // (a half-pasted secret is worse than nothing).
    ClipboardLimit int
}
```

### Security, stated plainly

The two directions have completely different risk profiles, and the defaults
should say so:

- **Write** (program → clipboard) is low-risk and should default ON. The
  worst case is clipboard spam from a program the user chose to run. This is
  the posture of kitty, foot and Windows Terminal.
- **Read** (`?` query) lets *any program that can print to the terminal* —
  including a `cat`ed file or a malicious script's output — silently
  exfiltrate whatever is on the clipboard, which is regularly a password.
  Default OFF, per xterm (`allowWindowOps` off) and kitty
  (`read-clipboard-ask`). A front end that wants it can enable it and put a
  confirmation UI in its callback; the `reply` function makes an async
  "ask the user first" flow natural.

Also worth honoring: when a write arrives while the user has text selected
*in the terminal itself*, the write wins (the program acted last). And the
event should fire on the parser's goroutine contract, whatever that is
documented to be — GTK and Qt both need to marshal to their UI threads, so
the callback should be documented as "any thread; marshal yourself."

## A parser bug worth fixing in the same visit

`handleOSCString` treats a bare ESC as the terminator and returns to ground
**without consuming the `\` of a real ST** (`ESC \`). The dangling `\` is
then processed from ground state as a printable and lands on screen. Every
ST-terminated OSC does this today; it goes mostly unnoticed because the 700x
family is BEL-terminated in practice. OSC 52 traffic from real programs is
overwhelmingly ST-terminated, so this stray-backslash bug will surface the
day OSC 52 lands. The fix is a one-state detour: on ESC inside an OSC string,
expect one more byte; if `\`, execute; if anything else, abort the OSC and
reprocess the byte.

## Sizing and non-goals

- **Payload size**: 1 MiB decoded default is generous (kitty defaults to
  8 MiB request limit; xterm historically far less). Configurable per above.
- **Non-goals for a first pass**: cut buffers (`0`–`7`), the `s`/`q`
  selections, multi-chunk protocols (OSC 52 has none; one sequence is one
  operation), and clipboard *formats* beyond text. All can be ignored
  without breaking any real program.

## What consumers do once this exists

- **GTK/Qt widgets**: wire the callback to the platform clipboard in the
  widget itself; ship enabled-for-write out of the box. Users of those
  widgets get working `vim "+y` with no code.
- **KittyTK trinket**: `SetOnClipboard` → `findDesktop().SetClipboard` /
  `ReadClipboardAsync`. Both ends of this are already built and waiting.
- **mew's guest sessions**: mew's `os_copy`, when running under a terminal
  (the TUI build inside anything), emits OSC 52 — and the whole nested stack
  lights up: inner mew → hosted PurfecTerm → KittyTK desktop clipboard, with
  each layer doing only its own job.
