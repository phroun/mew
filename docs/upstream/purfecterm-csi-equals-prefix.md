# Bug report + patch: `=` is a CSI private parameter prefix

*For the PurfecTerm maintainers. Found in mew/KittyTK on macOS against
v0.2.27. Affects every consumer — the `cli` adapter, the GTK widget, the Qt
widget — because the parser is shared.*

**An applyable patch is next to this file:
[`0001-purfecterm-csi-equals-prefix.patch`](0001-purfecterm-csi-equals-prefix.patch).**
`git am < 0001-purfecterm-csi-equals-prefix.patch` from the repository root,
or `git apply` / `patch -p1` to write your own commit message. It touches
`parser.go` (four lines of logic, the rest comment) and adds
`csiprivate_test.go`. Verified against v0.2.27: builds clean, the new tests
pass, and the reproduction below goes from leaking to clean.

---

## Symptom

Quit a full-screen program running inside PurfecTerm — vim, htop, anything
that uses the alternate screen — and this is left in the top-left corner,
sitting in front of the returning shell prompt:

```
0;1u
```

## Reproduction

```go
t, _ := cli.New(cli.Options{Cols: 40, Rows: 3, ScrollbackSize: 100, Embedded: true})
t.Feed([]byte("\x1b[=0;1u"))
// screen now contains: 0;1u
```

Across the family of sequences a TUI writes on the way out:

| fed | on screen (v0.2.27) |
|---|---|
| `ESC [ < u` | clean ✓ |
| `ESC [ > 1 u` | clean ✓ |
| `ESC [ = 0 ; 1 u` | **`0;1u`** ← the bug |
| `ESC [ ? 1049 l` `ESC [ < u` `ESC [ = 0 ; 1 u` | **`0;1u`** |
| `ESC [ ? 25 h` | clean ✓ |
| `ESC [ > 4 ; 2 m` | clean ✓ |
| `ESC [ 3 ; 4 Z` (unsupported final, no prefix) | clean ✓ |

Note the last row: an unsupported *final byte* is already handled correctly.
The hole is only an unrecognized *prefix*.

## Cause

`handleCSI` in `parser.go`:

```go
if p.state == stateCSI {
    // First byte after ESC [
    if b == '?' || b == '>' || b == '!' || b == '<' {
        p.csiPrivate = b
        p.state = stateCSIParam
        return
    }
    p.state = stateCSIParam
}
```

`'='` is missing. It is 0x3D — not a digit, not `;`, not `:`, and above the
0x20–0x2F intermediate range — so it falls all the way through to the
final-byte branch. The parser executes `CSI =` as an unknown final, returns to
ground, and the `0`, `;`, `1`, `u` that follow are printed as ordinary text.

## Why it shows up now

`CSI = 0 ; 1 u` is the Kitty keyboard protocol's "reset flags" — what a TUI
emits to undo `CSI > 1 u` as it exits. Any program that enables the Kitty
keyboard protocol writes it, and that is now most of them: kitty, foot, ghostty
and WezTerm all support the protocol, so libraries enable it by default and
reset it on teardown. A terminal that does not know `=` is a prefix will show
this every time such a program quits.

## Fix

The private parameter prefixes are the whole **0x3C–0x3F** range per ECMA-48 —
`<`, `=`, `>`, `?`. All four must be *recognized* even where the sequence
itself is unsupported, so that the parameters are consumed along with it
instead of printed:

```go
if b == '!' || (b >= 0x3C && b <= 0x3F) {
    p.csiPrivate = b
    p.state = stateCSIParam
    return
}
```

`'!'` (0x21) is an intermediate byte rather than a parameter prefix, but
`CSI ! p` (DECSTR) carries it in the prefix position, so it stays as-is.

## Related, from the OSC 52 request

The same "leaks the tail as text" shape appears in `handleOSCString`, which
treats a bare ESC as the terminator and returns to ground without consuming
the `\` of a real ST.

That one is **fixed in the follow-on patch**,
[`0002-purfecterm-osc52-clipboard.patch`](0002-purfecterm-osc52-clipboard.patch),
which applies on top of this one and also implements OSC 52 itself (it had to:
the sequence cannot work while ST drops a byte). Apply them in order:

```
git am 0001-purfecterm-csi-equals-prefix.patch 0002-purfecterm-osc52-clipboard.patch
```
