//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The whole KittyTK-side chain for ONE armed key: the desktop's pass-next-key
// check, the window's raw one-shot, the focus manager, the mew Editor trinket,
// and out the far end as the bytes that reach mew's stdin.
//
// F10 is the key this exists for. Every layer between here and there has its
// own claim on F10 -- the desktop menu bar, the window menu bar -- and arming
// raw key input is the statement that none of them get it this time.
func TestArmedF10ReachesTheMewSessionAsBytes(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("mew")
	e := NewEditor()
	var got []byte
	e.SetInputSink(func(b []byte) { got = append(got, b...) })
	win.SetContent(e)
	d.windowManager.AddWindow(win)

	a := &mockApp{name: "mew", main: win, windows: []*window.Window{win}}
	d.AddApplication(a)

	a.ActivatePassNextKeyToTrinket()
	d.HandleKeyPress(core.KeyPressEvent{Key: "F10"})

	if string(got) != "\x1b[21~" {
		t.Errorf("mew's stdin received %q, want %q (F10's encoding)", got, "\x1b[21~")
	}
}
