package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A horizontal splitter (vertical divider) shows the horizontal resize
// cursor; a vertical splitter (horizontal divider) shows the vertical one.
func TestSplitterCursorShape(t *testing.T) {
	if got := NewHSplitter().CursorShape(); got != core.CursorResizeH {
		t.Errorf("horizontal splitter cursor = %v, want CursorResizeH", got)
	}
	if got := NewVSplitter().CursorShape(); got != core.CursorResizeV {
		t.Errorf("vertical splitter cursor = %v, want CursorResizeV", got)
	}
}
