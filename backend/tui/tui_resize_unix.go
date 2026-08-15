//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/phroun/kittytk/core"
)

// handleResize listens for terminal resize signals (SIGWINCH) and queues
// a ResizeEvent when the window's cell dimensions change. POSIX only:
// SIGWINCH does not exist on Windows (see tui_resize_windows.go).
func (t *TUIBackend) handleResize() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)

	for {
		select {
		case <-sigChan:
			t.mu.Lock()
			if t.fd >= 0 && term.IsTerminal(t.fd) {
				cols, rows, err := term.GetSize(t.fd)
				if err == nil && (cols != t.cols || rows != t.rows) {
					t.cols = cols
					t.rows = rows
					t.allocateBuffers()

					// Set flag to clear each line on next render
					t.needsLineClear = true

					// Queue resize event
					event := core.ResizeEvent{
						Width:  t.metrics.CellToUnitsX(cols),
						Height: t.metrics.CellToUnitsY(rows),
						Cols:   cols,
						Rows:   rows,
					}
					select {
					case t.eventQueue <- event:
					default:
					}
				}
			}
			t.mu.Unlock()

		case <-t.stopChan:
			return
		}
	}
}
