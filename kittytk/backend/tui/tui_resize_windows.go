//go:build windows

package tui

// handleResize is a no-op on Windows: there is no SIGWINCH. The console
// can still be polled for size changes elsewhere; this goroutine just
// waits for shutdown so Stop() can join it cleanly, matching the POSIX
// build's lifecycle.
func (t *TUIBackend) handleResize() {
	<-t.stopChan
}
