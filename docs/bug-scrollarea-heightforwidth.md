# Bug: ScrollArea ignores height-for-width content

**Component:** `objects/trinkets/scrollarea.go`

## Summary

`ScrollArea` sizes its content purely from `content.SizeHint()` and does not
consult a content trinket's `HasHeightForWidth()` / `HeightForWidth()`. So
height-for-width content — e.g. a `Label` with `SetWordWrap(true)` — reports its
*unwrapped* width, the scroll area allocates that full width, the label never
wraps, and a horizontal scrollbar appears. The desired behavior is the content
wrapping to the viewport width and scrolling only vertically.

## Where

`updateScrollBars()` (~line 1099):

```go
hint := s.content.SizeHint()
s.contentWidth = hint.Width      // a wrapped Label reports its longest UNWRAPPED line
s.contentHeight = hint.Height
```

## Related

`SetTrinketResizable(true)` clamps *both* dimensions to the viewport (~line 1306:
`contentBounds.Width = viewport.Width; contentBounds.Height = viewport.Height`).
That makes wrapped content fit the width (so it wraps) but also clamps its height
to the viewport, killing vertical scroll. So there is currently **no mode** that
does "fit width, natural height-for-width height" — the combination needed for a
scrollable wrapped-text view.

## Repro

Place a `Label` with `SetWordWrap(true)` and a few long lines as a `ScrollArea`'s
content:

- default: horizontal scrollbar, no wrapping;
- with `SetTrinketResizable(true)`: wraps, but won't scroll vertically past the
  viewport.

## Suggested fix

In the content-sizing path, when `content.HasHeightForWidth()` is true, set
`contentWidth = viewportWidth` and
`contentHeight = content.HeightForWidth(viewportWidth)` (accounting for the
vertical scrollbar's column). Equivalently, add a "fit-width-only" resize mode
distinct from `trinketResizable` (which fits both dimensions).

## Workaround in use

The mew editor placeholder trinket (`objects/trinkets/editor.go`, `!mew` build)
pre-wraps its text to the scroll's viewport width and feeds a non-wrapping label,
so `SizeHint` already reflects the wrapped size. Once this is fixed upstream, the
placeholder can drop the pre-wrapping and just use a wrapped label.
