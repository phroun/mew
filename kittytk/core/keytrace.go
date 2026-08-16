package core

import (
	"fmt"
	"os"
	"sync"
)

// Key tracing records a keystroke's trip from a backend to a hosted guest, and
// exists because that trip crosses five layers that each looked correct alone.
//
// A key release in particular has to survive: a backend producing it, the
// desktop routing it, a focus scope and a window carrying it, a trinket
// forwarding it, and an emulator encoding it — and the encoding only happens at
// all if the guest negotiated event reporting. A break at any one of those is
// invisible from either end, because the symptom is identical: nothing arrives.
// Reasoning inward from "no key-up in the browser" found four of the five and
// stopped one short each time. The trace shows which layer the event reached.
//
// It writes to the file named by KITTYTK_KEY_TRACE and is inert when that
// variable is unset. A file rather than stderr: on the TUI backend stderr IS
// the screen, and a line written there lands in the middle of the paint.
var (
	keyTraceOnce sync.Once
	keyTraceMu   sync.Mutex
	keyTraceFile *os.File
)

func keyTraceSink() *os.File {
	keyTraceOnce.Do(func() {
		path := os.Getenv("KITTYTK_KEY_TRACE")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		keyTraceFile = f
	})
	return keyTraceFile
}

// KeyTracing reports whether a sink is configured, so a caller can skip
// assembling an argument list that would only be thrown away.
func KeyTracing() bool { return keyTraceSink() != nil }

// KeyTracef appends one line to the trace when one is configured, and does
// nothing otherwise.
func KeyTracef(format string, args ...any) {
	f := keyTraceSink()
	if f == nil {
		return
	}
	keyTraceMu.Lock()
	defer keyTraceMu.Unlock()
	fmt.Fprintf(f, format+"\n", args...)
}
