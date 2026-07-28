//go:build windows

package render

import "time"

// watchNativeResize POLLS the terminal size on Windows, because there is no
// signal to wait for. SIGWINCH does not exist there, and until this the
// renderer simply never learned that its terminal had changed shape: a mew
// running inside anything that could resize it — a pseudoconsole, a terminal
// window — kept drawing to the size it started with, and every line came out
// askew the moment the width moved.
//
// A quarter second is far below noticing and far above costing anything: two
// integers compared, and only when they differ does anything happen. The
// query is the renderer's own sizeFn, so a virtualized terminal is polled
// exactly as a real one is.
func watchNativeResize(sr *ScreenRenderer) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(resizePollInterval)
		defer t.Stop()
		lastW, lastH, _ := sr.sizeFn()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				w, h, err := sr.sizeFn()
				if err != nil || (w == lastW && h == lastH) {
					continue
				}
				lastW, lastH = w, h
				sr.TriggerResize()
			}
		}
	}()
	return func() { close(stop) }
}

// resizePollInterval is how often the size is asked for. Windows offers no
// notification, so this is the whole mechanism.
const resizePollInterval = 250 * time.Millisecond
