//go:build windows

package tui

// SuspendHost reports false: Windows has no job control to suspend into, so
// host_suspend does nothing here.
func (t *TUIBackend) SuspendHost() bool { return false }

// watchJobControl has nothing to watch on Windows, which has no SIGTSTP or
// SIGCONT.
func (t *TUIBackend) watchJobControl() {}
