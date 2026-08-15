//go:build mew

package trinkets

import "testing"

// fakeSess is a PTY session that records what it was resized to. The cell-only
// Resize and the pixel-carrying one are recorded separately so a test can tell
// which was taken.
type fakeSess struct {
	cols, rows       int
	pxW, pxH         int
	cellOnlyResizes  int
	pixelResizes     int
	supportsPixelsIn bool
}

func (f *fakeSess) Read(p []byte) (int, error)  { return 0, nil }
func (f *fakeSess) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeSess) Close() error                { return nil }

func (f *fakeSess) Resize(cols, rows int) error {
	f.cols, f.rows = cols, rows
	f.cellOnlyResizes++
	return nil
}

// pixelSess additionally accepts pixel dimensions (purfecterm's PTY from
// v0.2.44 on).
type pixelSess struct{ fakeSess }

func (f *pixelSess) ResizeWithPixels(cols, rows, w, h int) error {
	f.cols, f.rows, f.pxW, f.pxH = cols, rows, w, h
	f.pixelResizes++
	return nil
}

// A program drawing pictures sizes its viewport from TIOCGWINSZ rather than
// asking the terminal, so a window reported as 0x0 pixels leaves it running
// and rendering nothing. When the session can carry pixels, every resize
// carries them.
func TestResizeCarriesPixelWinsize(t *testing.T) {
	s := &pixelSess{}
	// The pane's MEASURED extent, which is deliberately not cols*cellW: the
	// grid is fitted on the unscaled cell and the scrollbar lane is already
	// out of it, so the product would overstate both.
	wrapped := withPixelWinsize(s, func() (int, int) { return 553, 361 })
	if err := wrapped.Resize(80, 24); err != nil {
		t.Fatal(err)
	}
	if s.pixelResizes != 1 || s.cellOnlyResizes != 0 {
		t.Errorf("took the cell-only path: pixel=%d cell=%d", s.pixelResizes, s.cellOnlyResizes)
	}
	if s.cols != 80 || s.rows != 24 {
		t.Errorf("cells = %dx%d, want 80x24", s.cols, s.rows)
	}
	if s.pxW != 553 || s.pxH != 361 {
		t.Errorf("pixels = %dx%d, want the measured 553x361", s.pxW, s.pxH)
	}
}

// Before the first paint there is no measured extent. Reporting a
// window of zero pixels is the very failure this exists to prevent, so the
// cell-only call is used instead and the pixel fields keep whatever the PTY
// already had.
func TestUnknownCellSizeFallsBackToCellsOnly(t *testing.T) {
	s := &pixelSess{}
	wrapped := withPixelWinsize(s, func() (int, int) { return 0, 0 })
	if err := wrapped.Resize(80, 24); err != nil {
		t.Fatal(err)
	}
	if s.pixelResizes != 0 || s.cellOnlyResizes != 1 {
		t.Errorf("reported a zero-pixel window: pixel=%d cell=%d", s.pixelResizes, s.cellOnlyResizes)
	}
}

// A session that cannot carry pixels — a Windows pseudoconsole, or a PTY
// older than v0.2.44 — is handed back untouched rather than wrapped.
func TestSessionWithoutPixelSupportIsNotWrapped(t *testing.T) {
	s := &fakeSess{}
	if got := withPixelWinsize(s, func() (int, int) { return 553, 361 }); got != mewSess(s) {
		t.Error("a session with no pixel resize was wrapped anyway")
	}
	if err := s.Resize(80, 24); err != nil {
		t.Fatal(err)
	}
	if s.cellOnlyResizes != 1 {
		t.Errorf("cell-only resizes = %d, want 1", s.cellOnlyResizes)
	}
}

// mewSess is an identity helper so the comparison above is between interface
// values of the same dynamic type.
func mewSess(s *fakeSess) interface{ Resize(int, int) error } { return s }
