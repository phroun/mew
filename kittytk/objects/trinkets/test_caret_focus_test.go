package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A trinket keeps its local focus when its window goes to the
// background; the focus chain must report inactive then so the caret
// is not drawn in two windows at once.
func TestFocusChainActiveTracksWindowActivation(t *testing.T) {
	input := NewTextInput()
	win := window.NewWindow("host")
	win.SetContent(input)

	win.SetActive(true)
	if !core.FocusChainActive(input.Self()) {
		t.Fatal("active window: chain should be active")
	}

	win.SetActive(false)
	if core.FocusChainActive(input.Self()) {
		t.Fatal("inactive window: chain must report inactive")
	}
}
