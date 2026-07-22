# direct-key-handler patch: horizontal scroll wheel

`hscroll.patch` teaches the SGR mouse decoder to distinguish the horizontal
scroll wheel. Verified against `github.com/phroun/direct-key-handler@v0.3.7`:
applies clean, and the patched tree builds and passes `go test ./keyboard/`.

## The bug

`formatMouseEvent` (keyboard/handler.go) decodes the scroll wheel from the SGR
button code but only handles two directions:

```go
if isScroll {
    if buttonBits == 0 {
        action = "MouseScrollUp"
    } else {
        action = "MouseScrollDown"   // <-- everything that isn't up
    }
}
```

The wheel axis/direction is the low two bits of the button code: 0 = up,
1 = down, **2 = left, 3 = right** (SGR buttons 64..67). Collapsing 2 and 3
onto "MouseScrollDown" means a horizontal wheel / two-finger sideways gesture
scrolls the view **down** instead of sideways.

## The fix

Replace the up/else with a full four-way switch, emitting the new actions
`MouseScrollLeft` and `MouseScrollRight` for buttons 66 and 67. Adds
`keyboard/mousescroll_test.go` locking all four directions.

## Applying

From the direct-key-handler checkout (at v0.3.7):

    git apply hscroll.patch
    go build ./... && go test ./keyboard/

## Consumer note

mew currently works around the missing decode by plucking the horizontal SGR
reports (buttons 66/67) out of the byte stream itself, before this decoder
(`internal/input/mousehscroll.go`), and synthesizing `MouseScrollLeft`/
`MouseScrollRight`. Once this patch ships in a release and mew adopts it, that
workaround can be retired — the decoder will emit the two actions directly.
