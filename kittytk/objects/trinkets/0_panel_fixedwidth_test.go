package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// fixed_width pins the panel's SizeHint width so a wrapping label inside
// wraps at exactly that width (the box overflows rather than the text
// shrinking), while height still flows from content - the behavior the
// demo's Selection boxes rely on.
func TestPanelFixedWidthPinsSizeHint(t *testing.T) {
	p := NewPanel()
	p.SetBorder(true)
	p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	lbl := NewLabel("The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence")
	lbl.SetWordWrap(true)
	p.AddChild(lbl)

	natural := p.SizeHint().Width

	p.SetFixedWidth(256)
	if got := p.SizeHint().Width; got != 256 {
		t.Errorf("pinned SizeHint width = %d, want 256 (natural was %d)", got, natural)
	}

	// Height flows: narrower widths wrap onto more lines and grow taller.
	if !p.HasHeightForWidth() {
		t.Fatal("expected height-for-width with a wrapping label")
	}
	tall := p.HeightForWidth(160)
	short := p.HeightForWidth(2000)
	if tall <= short {
		t.Errorf("HeightForWidth(160)=%d not taller than HeightForWidth(2000)=%d", tall, short)
	}

	// Clearing restores the natural (content-derived) width.
	p.SetFixedWidth(0)
	if got := p.SizeHint().Width; got != natural {
		t.Errorf("cleared SizeHint width = %d, want natural %d", got, natural)
	}
}
