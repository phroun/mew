package core

import (
	"fmt"
	"os"
	"sync"
)

// Mouse tracing is a diagnostic for the pointer's trip from the outer terminal
// to a hosted guest, and it exists because the two halves of that trip do not
// carry their coordinates the same way.
//
// A motion report arrives with its position embedded in the event itself; a
// press or release arrives as two events, a position followed by an action,
// and the position is stashed until the action reads it. Hover and click can
// therefore disagree while every function between them is correct in
// isolation — which is not a difference any single-end test will show. The
// trace records both ends of the same gesture so they can be compared.
//
// It writes to the file named by KITTYTK_MOUSE_TRACE and is inert when that
// variable is unset. A file rather than stderr: on the TUI backend stderr IS
// the screen, and a trace line written there lands in the middle of the paint.
var (
	mouseTraceOnce sync.Once
	mouseTraceMu   sync.Mutex
	mouseTraceFile *os.File
)

func mouseTraceSink() *os.File {
	mouseTraceOnce.Do(func() {
		path := os.Getenv("KITTYTK_MOUSE_TRACE")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		mouseTraceFile = f
	})
	return mouseTraceFile
}

// MouseTracing reports whether a sink is configured, so a caller can skip
// assembling an argument list that would only be thrown away. Mouse events
// arrive at motion rates, so the untraced path must cost a nil check.
func MouseTracing() bool { return mouseTraceSink() != nil }

// MouseTracef appends one line to the trace when one is configured, and does
// nothing otherwise.
func MouseTracef(format string, args ...any) {
	f := mouseTraceSink()
	if f == nil {
		return
	}
	mouseTraceMu.Lock()
	defer mouseTraceMu.Unlock()
	fmt.Fprintf(f, format+"\n", args...)
}
