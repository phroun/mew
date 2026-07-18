//go:build !unix

package editor

import (
	"os"
	"syscall"
)

// deadcatSignals off unix: SIGTERM is the portable external-kill signal
// (SIGHUP has no cross-platform meaning here).
func deadcatSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM}
}
