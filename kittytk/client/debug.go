package client

// Optional client-side tracing: set KITTYTK_DEBUG=1 to log dial and
// request/reply progress to stderr. Off by default and free when off.

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
		fmt.Fprintf(os.Stderr, "kittytk/client: "+format+"\n", args...)
	}
}
