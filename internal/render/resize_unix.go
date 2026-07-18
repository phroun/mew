//go:build unix

package render

import (
	"os"
	"os/signal"
	"syscall"
)

// watchNativeResize installs the OS terminal-resize watcher (SIGWINCH) and
// forwards resize signals into the renderer's resize channel. It returns a
// function that uninstalls the watcher. Only built on unix-like platforms;
// elsewhere hosts drive resizes via TriggerResize or a resize channel.
func watchNativeResize(sr *ScreenRenderer) func() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	go func() {
		for range sigCh {
			sr.TriggerResize()
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(sigCh)
	}
}
