# ifitfits

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **ifitfits** — *a viewport tiling engine ("if it fits, it shows")*

*If you use this, please support me on ko-fi:  [https://ko-fi.com/jeffday](https://ko-fi.com/F2F61JR2B4)*

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/F2F61JR2B4)

A small, renderer-agnostic tiling engine in Go. It maintains an oriented tree of
**tiles** and resolves it to boxes by a space-negotiation waterfall, with tabbed
**stacks**, magnify **lenses**, and a **caret** that steers directional
navigation. It draws nothing and knows nothing about pixels or cells — it only
does arithmetic on the numbers a host gives it — so the same engine drives a
terminal desktop or a graphical one.

It's a sibling to the rest of the line: **Mew** (text editor), **PurfecTerm**
(terminal emulator), **PawScript** (language), and **KittyTK** (UI toolkit).
The name reads *"if it fits"* — a window shows if there's room for it; when the
workspace runs out, the lowest-priority tiles are omitted, cleanly.

## Model

- A **tile** is a leaf of the tree with a unique, library-assigned **handle** —
  the id every command takes. A tile also carries a **ref**: host content
  identity, which (unlike a handle) may be shared across many tiles.
- Space is allocated along each group's axis by a waterfall:
  **minimums → pins → zoom → natural → mop-up**, with a cross-axis omission pass.
  Overflow is resolved by priority; a stack reports the *max* of its tabs, so
  flipping a tab never triggers a relayout.
- The engine does **not** own focus — that's the host's job. Every command takes
  an explicit tile; the host reports focus (`SetFocus`) only so a lens knows when
  to dismiss.

## Use

```go
import "github.com/phroun/ifitfits"

vp, first := ifitfits.NewViewport(1920, 1080) // one tile, filling the workspace
vp.Set(first, "editor")                        // give it a content ref

right := vp.Split(first, ifitfits.Right)        // split; clones "editor"
vp.Set(right, "terminal")
vp.Stack(right, ifitfits.On)                    // fold its group into tabs

for _, b := range vp.Tiles() {                  // resolved boxes, for the host to draw
    draw(b.Ref, b.Rect)
}

dest := vp.Go(first, ifitfits.Right)            // navigate; returns the destination tile
vp.Monocle(dest, ifitfits.On)                   // magnify it to fill the screen
```

The full command surface — and the **mew** (PawScript) `viewport_*` names each
method mirrors — is in [COMMANDS.md](COMMANDS.md).

## Test

```
go test ./...
```

## License

MIT — see [LICENSE](LICENSE).
