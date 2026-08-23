//go:build windows

package tui

// ttyPixelSize has no Windows equivalent: a console has no pixel dimensions to
// report, and nothing there speaks a terminal graphics protocol either.
func ttyPixelSize(int) (wPx, hPx int) { return 0, 0 }
