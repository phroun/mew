package window

// The window half of the close trace: see objects/trinkets/closetrace.go, which
// explains why this exists. Same switch, KITTYTK_TRACE_CLOSE=1, so one setting gets the
// whole path -- the question being asked here and where the desktop puts it there.

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

var closeTraceOn atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITTYTK_TRACE_CLOSE"))) {
	case "1", "true", "yes", "on":
		closeTraceOn.Store(true)
	}
}

func closeTrace(format string, args ...any) {
	if closeTraceOn.Load() {
		fmt.Fprintf(os.Stderr, "kittytk/close: "+format+"\n", args...)
	}
}

// AskForceClose's own walk reports what it found, because a window that cannot find the
// desktop asks nobody and says nothing -- which looks exactly like a window that was
// never asked about.
func (w *Window) traceCoordinator() {
	if !closeTraceOn.Load() {
		return
	}
	found := w.findCloseCoordinator() != nil
	closeTrace("AskForceClose: %q(id=%d) found a desktop to ask through: %v",
		w.Title(), w.ObjectID(), found)
}
