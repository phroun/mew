# Bug report + patch: a second ESC must restart the sequence

*For the direct-key-handler maintainers. Found in mew/KittyTK on macOS,
v0.3.9. Affects every consumer that enables mouse reporting, and any that
runs where two sequences can land in one read.*

---

## Symptom

Press Escape while the pointer is over the window, and this lands in the
document as literal text:

```
[<35;12;5M
```

That is an SGR mouse report (`ESC [ < 35 ; 12 ; 5 M` — motion, no button, at
column 12, row 5) with its ESC eaten and the rest typed.

## Reproduction (runnable, no terminal required)

```go
h := keyboard.New(keyboard.Options{InputReader: pr})
h.Start()
pw.Write([]byte("\x1b" + "\x1b[<35;12;5M"))   // Escape, then a mouse report
```

Collected from `h.Keys`:

| input | keys emitted |
|---|---|
| `"\x1b\x1b[<35;12;5M"` | `Escape Escape [ < 3 5 ; 1 2 ; 5 M` ← **wrong** |
| `"\x1b"` alone | `Escape` ✓ |
| `"\x1b[<35;12;5M"` alone | `MouseDrag@12,5` ✓ |
| `"\x1b\x1b[A"` | `Special M-Up` ← also wrong |

Each half parses correctly on its own. Together they do not.

## Cause

In `processByte`, the `h.inEscape` branch appends **every** byte to
`escBuffer`, including a second `0x1b`:

```go
if h.inEscape {
    h.escBuffer = append(h.escBuffer, b)   // <- a second ESC lands here too
    seq := string(h.escBuffer)
    ...
}
```

So a lone ESC followed by any sequence accumulates `"\x1b\x1b[<35;12;5M"`,
matches no binding, is not a valid prefix, fails `parseModifiedCSI` (the
leading double ESC), and falls through to `emitEscapeBuffer()` — which emits
the buffer byte by byte. The user's Escape is delivered, and the mouse
report's body is typed as text.

The 50ms escape timer does not save it: the two arrive in the same read, so
no gap exists to time out on.

## Why it shows up now

This needs a lone ESC immediately followed by another sequence, which needs
something *else* emitting sequences. Mouse reporting is that something: with
all-motion tracking (DECSET 1003) on, any pointer movement produces a report,
so pressing Escape with the pointer over the window is enough. Consumers
without mouse reporting will rarely see it; consumers with it will see it
regularly, and it will look like random garbage in a document.

## Fix

ESC is the one byte that always restarts. Every VT parser treats it that way
(the ANSI state machine has ESC as an unconditional transition to escape
entry, from every state). Handle it before the append:

```go
if h.inEscape {
    // A second ESC cannot occur INSIDE any sequence: it terminates the
    // pending one and begins a new one. Without this, a lone Escape
    // followed by any sequence swallows both and emits the second one's
    // body as text.
    if b == 0x1b {
        h.emitEscapeBuffer()          // resolve what we had (a lone ESC -> Escape)
        h.escBuffer = []byte{b}
        h.inEscape = true
        escTimeout.Reset(50 * time.Millisecond)
        return
    }
    h.escBuffer = append(h.escBuffer, b)
    ...
}
```

`emitEscapeBuffer` already does the right thing for the common case: a buffer
holding just `ESC` emits `Escape`. For a partial sequence it emits what it
has, which is the existing behaviour for anything unparseable — no worse than
today, and the new sequence still starts clean.

Worth checking `emitEscapeBuffer` maps a bare `ESC` to the `Escape` key name
rather than a literal; if it does not, special-case that one length.

## Note on the same area

`"\x1b\x1b[A"` emits `Special` then `M-Up`. Both are wrong (it should be
`Escape` then `Up`), and both come from the same accumulation. The patch above
fixes them together, so it is worth adding all four rows of the table as test
cases.

## Test suggestion

```go
func TestEscRestartsTheSequence(t *testing.T) {
    for _, tc := range []struct{ in string; want []string }{
        {"\x1b",                 []string{"Escape"}},
        {"\x1b[<35;12;5M",       []string{"MouseDrag@12,5"}},
        {"\x1b\x1b[<35;12;5M",   []string{"Escape", "MouseDrag@12,5"}},
        {"\x1b\x1b[A",           []string{"Escape", "Up"}},
        {"\x1b\x1b",             []string{"Escape", "Escape"}},
    } { ... }
}
```

The third row is the reported bug; the last covers a double-tap of Escape,
which some editors bind and which today produces `Special`.
