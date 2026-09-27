//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// SuspendHost hands the terminal back to the shell and stops the process, as a
// job-control shell's suspend would, returning once the process has been
// continued and the terminal taken back. It reports false, having done
// nothing, when there is nothing to suspend to: job control turned off, no
// terminal, or a SIGTSTP that was already ignored when the process started,
// which is how a parent without job control launches its children.
//
// Called on the event loop, it blocks the loop for as long as the process is
// stopped, which is no time at all from the loop's point of view.
func (t *TUIBackend) SuspendHost() bool {
	t.mu.Lock()
	fd := t.fd
	t.mu.Unlock()
	if !t.jobControl || fd < 0 || !term.IsTerminal(fd) || signal.Ignored(syscall.SIGTSTP) {
		return false
	}
	t.suspend(t.stopProcess)
	return true
}

// stopProcess stops the whole process group and returns when it is
// continued. The group, not only this process, because that is the job the
// shell sees: stopping one member would leave the shell waiting on the others.
// Children a terminal trinket runs sit in sessions of their own, so it does not
// reach them.
//
// The stop is SIGSTOP, not the SIGTSTP a shell's suspend key sends. Once the
// watcher has asked the Go runtime for SIGTSTP, the runtime's handler stays
// installed even after signal.Reset, so a SIGTSTP sent here would come back to
// the watcher as another request to suspend instead of stopping anything.
// SIGSTOP cannot be caught, so it always stops. The shell reports it as a stop
// by signal rather than by the keyboard, and fg continues it the same way.
func (t *TUIBackend) stopProcess() {
	t.ownConts.Add(1)
	syscall.Kill(0, syscall.SIGSTOP)
}

// watchJobControl answers the two job-control signals that arrive from
// outside.
//
// SIGTSTP is a request to suspend, from kill -TSTP or a wrapper passing job
// control through, and gets the same suspend host_suspend does. Left to its
// default it would stop the process on the spot, with the terminal still raw
// and on the alternate screen, and the shell would come back to that.
//
// SIGCONT after a stop this process sent itself is already handled by the
// suspend that sent it, and is skipped. Any other SIGCONT follows a SIGSTOP,
// which cannot be caught, so nothing handed the terminal back first and the
// shell may have reset it in the meantime. The modes are turned off and on
// again, with no stop between, so the terminal ends up in the state this
// backend believes it is in and the screen is written afresh.
func (t *TUIBackend) watchJobControl() {
	if signal.Ignored(syscall.SIGTSTP) {
		return // no job control to take part in
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGTSTP, syscall.SIGCONT)
	defer signal.Stop(ch)

	for {
		select {
		case sig := <-ch:
			switch sig {
			case syscall.SIGTSTP:
				t.suspend(t.stopProcess)
			case syscall.SIGCONT:
				if t.ownConts.Load() > 0 {
					t.ownConts.Add(-1)
					continue
				}
				t.suspend(func() {})
			}
		case <-t.stopChan:
			return
		}
	}
}
