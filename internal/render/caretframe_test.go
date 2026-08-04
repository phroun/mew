package render

import "testing"

// TestCaretRowGeomHonorsFrame checks that caret/ghost/off-screen geometry is
// measured within the viewport's horizontal frame: offsetting the frame moves
// the base column by FrameX, and narrowing it reduces the content width by the
// same amount. (Differences cancel the gutter/scrollbar/margin terms, so the
// test does not depend on those.)
func TestCaretRowGeomHonorsFrame(t *testing.T) {
	sr, w := testRenderer()
	sr.Width = 80

	base0, _, _, cw0, _ := sr.caretRowGeom(w, "")

	w.FrameX = 10
	w.FrameWidth = 30
	base1, _, _, cw1, _ := sr.caretRowGeom(w, "")

	if base1-base0 != 10 {
		t.Fatalf("base shifted by %d, want 10 (FrameX)", base1-base0)
	}
	if cw0-cw1 != sr.Width-30 {
		t.Fatalf("content width shrank by %d, want %d (screen %d → frame 30)", cw0-cw1, sr.Width-30, sr.Width)
	}
}
