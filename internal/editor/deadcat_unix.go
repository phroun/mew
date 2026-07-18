//go:build unix

package editor

import (
	"os"
	"syscall"
)

// deadcatSignals are the catchable deaths that trigger a DEADCAT dump on unix:
// SIGHUP (the terminal or SSH session went away) and SIGTERM (an external
// kill). In mew's raw terminal mode, Ctrl-C / Ctrl-\ arrive as bytes rather
// than SIGINT / SIGQUIT, so those are not useful triggers here.
func deadcatSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP, syscall.SIGTERM}
}
