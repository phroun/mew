package tui

import "sync"

// The terminal is PROCESS state. A TUI backend puts it into raw mode, the
// alternate screen and the "kitty" keyboard protocol, and something has to put
// it back — but the code that ends the process is not always the code holding
// the backend. An embedder's fatal-signal path is the usual case: mew dumps
// its unsaved buffers and calls os.Exit, and os.Exit runs no deferred
// teardown, so the backend's Shutdown never happens and the user is returned
// to a shell in raw mode with the "kitty" keyboard protocol still on.
//
// The registry closes that gap without the backend having to fight for signals
// it does not own. Installing our own handler would race the embedder's: mew's
// runs on SIGINT/SIGTERM to write out unsaved work, and a handler here that
// restored and re-raised could kill the process mid-dump — losing the user's
// edits to tidy up their prompt, which is a bad trade. Instead every live
// backend registers itself, and ANY path that is about to end the process
// calls RestoreAll, whether or not it can reach a backend.
var (
	liveMu  sync.Mutex
	live    = map[*TUIBackend]struct{}{}
	liveSeq int
)

// registerLive adds a backend to the set RestoreAll walks. Called once the
// terminal has actually been put into its raw/alt-screen state, so a backend
// that failed to initialize is never asked to undo what it never did.
func registerLive(t *TUIBackend) {
	liveMu.Lock()
	defer liveMu.Unlock()
	live[t] = struct{}{}
	liveSeq++
}

// unregisterLive drops a backend that has already restored the terminal.
func unregisterLive(t *TUIBackend) {
	liveMu.Lock()
	defer liveMu.Unlock()
	delete(live, t)
}

// RestoreAll hands the terminal back from every backend that currently holds
// it. It is the call for an exit path that cannot use the normal teardown —
// a fatal-signal handler, a panic recovery, anything ahead of os.Exit — and it
// is safe from any goroutine, safe to call when no backend is running, and
// safe to call repeatedly: each backend restores at most once (RestoreTerminal
// is guarded), and calling it does NOT stop the backend or exit the process.
//
// Deliberately does not exit. The caller owns exit policy: an embedder that
// still has unsaved work to write must be able to restore the terminal first
// and finish its own job afterwards.
func RestoreAll() {
	liveMu.Lock()
	backends := make([]*TUIBackend, 0, len(live))
	for t := range live {
		backends = append(backends, t)
	}
	liveMu.Unlock()

	// Outside the lock: RestoreTerminal opens /dev/tty and writes, and a
	// caller in a signal handler should not be able to deadlock on the
	// registry if a backend is being torn down concurrently.
	for _, t := range backends {
		t.RestoreTerminal()
	}
}

// LiveCount reports how many backends currently hold the terminal. For tests
// and for an embedder that wants to know whether restoring is its problem.
func LiveCount() int {
	liveMu.Lock()
	defer liveMu.Unlock()
	return len(live)
}
