package window

import (
	"testing"

	"github.com/phroun/kittytk/style"
)

// The cell surface spends its quarter of black as ink: the light-shade block
// over the frame's own colour. It takes the frame's COLOUR and none of its
// attributes -- a title bar is bold, and a terminal renders bold black as
// bright black, which is grey on blue rather than the black on blue the shade
// is meant to be.
func TestTheCellShadeCarriesNoTitleBarAttributes(t *testing.T) {
	title := style.DefaultStyle().WithFg(style.ColorWhite).WithBg(style.ColorBlue).Bold()
	if title.Attrs&style.StyleBold == 0 {
		t.Fatal("the title style under test is not bold; this proves nothing")
	}

	ink := shadeInk(title)
	if ink.Attrs != style.StyleNormal {
		t.Errorf("the shade carries attributes %v; bold black renders as bright black", ink.Attrs)
	}
	if ink.Fg != style.ColorBlack {
		t.Errorf("the shade's ink is %v, want black", ink.Fg)
	}
	if ink.Bg != title.Bg {
		t.Errorf("the shade's ground is %v, want the frame's own %v", ink.Bg, title.Bg)
	}
}
