package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// wrappingContent is a panel of wrapped text: the thing whose height depends
// on the width it is given.
func wrappingContent(text string) *Panel {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	l := NewLabel(text)
	l.SetWordWrap(true)
	p.AddChild(l)
	return p
}

// A scroll area whose content tracks the viewport width has to measure that
// content at the width it is GIVEN. Measured at the width it would have
// liked, a paragraph reflowing into a narrower viewport reports fewer lines
// than it draws, and the scroll range stops short of its own last line --
// the bottom of the text is unreachable.
func TestAScrollAreaMeasuresWrappedContentAtTheWidthItGivesIt(t *testing.T) {
	long := "Wrapped text that runs on for a good long while so that a narrow " +
		"viewport has to break it into a great many more lines than the width " +
		"it would have chosen for itself ever would, which is the whole point."

	sa := NewScrollArea()
	sa.SetTrinketResizable(true)
	sa.SetContent(wrappingContent(long))
	// Narrow and short: the text must wrap well past the bottom.
	sa.SetBounds(core.UnitRect{Width: 160, Height: 64})
	sa.Layout()

	if !sa.needsVScrollBar() {
		t.Fatal("the wrapped text does not overflow its viewport; the test proves nothing")
	}
	if max := sa.vScrollBar.Maximum(); max <= 0 {
		t.Errorf("the vertical scroll range is %d: there is nowhere to scroll to, so the "+
			"bottom of the wrapped text cannot be reached", max)
	}

	// The range has to cover the whole of the text: scrolled to the end, the
	// last line is inside the viewport.
	viewport := sa.viewportBounds()
	reach := sa.contentHeight - viewport.Height
	if reach <= 0 {
		t.Fatalf("content %d tall in a %d viewport leaves nothing to scroll",
			sa.contentHeight, viewport.Height)
	}

	// And the same content in a WIDE area needs fewer lines, so the two
	// measurements must differ -- which is what says the width was consulted.
	wide := NewScrollArea()
	wide.SetTrinketResizable(true)
	wide.SetContent(wrappingContent(long))
	wide.SetBounds(core.UnitRect{Width: 640, Height: 64})
	wide.Layout()
	if wide.contentHeight >= sa.contentHeight {
		t.Errorf("the same text measures %d tall at 640 wide and %d at 160; a wider "+
			"viewport must need fewer lines", wide.contentHeight, sa.contentHeight)
	}
}

// Content that does not wrap is unaffected: its height comes from its hint,
// as it always did.
func TestAScrollAreaLeavesFixedContentAlone(t *testing.T) {
	sa := NewScrollArea()
	sa.SetTrinketResizable(true)
	inner := NewPanel()
	inner.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	inner.AddChild(NewLabel("one line"))
	sa.SetContent(inner)
	sa.SetBounds(core.UnitRect{Width: 160, Height: 64})
	sa.Layout()

	if got, want := sa.contentHeight, inner.SizeHint().Height; got != want {
		t.Errorf("content that does not wrap measured %d, want its hint's %d", got, want)
	}
}
