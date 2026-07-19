package display

// Optional host-side tracing: set KITTYTK_DEBUG=1 to log connection
// admission, prompt decisions, handshake, and batch execution to stderr.
// Off by default and free when off.

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

var debugOn atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITTYTK_DEBUG"))) {
	case "1", "true", "yes", "on":
		debugOn.Store(true)
	}
}

func dbg(format string, args ...any) {
	if debugOn.Load() {
		fmt.Fprintf(os.Stderr, "kittytk: "+format+"\n", args...)
	}
}
