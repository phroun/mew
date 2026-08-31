package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// comboOn returns a combo box on a graphical surface, narrow enough that its
// current text does not fit beside the arrow.
func comboOn(t *testing.T, width core.Unit, item string) (*ComboBox, *raster.Backend) {
	t.Helper()
	b, err := raster.New(400, 40)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)
	c := NewComboBox()
	c.AddItem(item)
	c.SetCurrentIndex(0)
	c.SetParent(d)
	c.SetBounds(core.UnitRect{Width: width, Height: 16})
	return c, b
}

// A value too wide for the box is cut back to fit beside the arrow. It used
// to be cut a BYTE at a time with nothing appended, so "ARJ Archive" came out
// "ARJ Archiv" -- a word that just looks misspelled, and on any multi-byte
// character half a rune handed to the renderer.
func TestComboBoxEllipsizesRatherThanChopping(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const value = "ARJ Archive"
	c, _ := comboOn(t, 400, value)
	full := c.MeasureText(value) + c.MeasureText(" ▼")

	// A box a few units short of holding the whole value.
	c, b := comboOn(t, full-12, value)
	b.Clear(style.DefaultStyle())
	c.Paint(core.NewPainter(b))

	shown := c.displayText(value, (full-12)-c.MeasureText(" ▼"))
	if shown == value {
		t.Fatalf("the fixture is not narrow enough: %q fits", value)
	}
	if !strings.HasSuffix(shown, "…") {
		t.Errorf("cut text is %q; a cut has to say so", shown)
	}
	if strings.HasPrefix(value, shown) {
		t.Errorf("cut text %q is just a prefix of %q -- nothing marks the cut", shown, value)
	}
}

// Rune safety: a multi-byte value must never be split mid-rune.
func TestComboBoxDoesNotSplitARune(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const value = "ααααααααααααααα"
	c, b := comboOn(t, 60, value)
	b.Clear(style.DefaultStyle())
	c.Paint(core.NewPainter(b))

	shown := c.displayText(value, 60-c.MeasureText(" ▼"))
	for _, r := range shown {
		if r == '�' {
			t.Fatalf("mid-rune split: %q", shown)
		}
	}
}
