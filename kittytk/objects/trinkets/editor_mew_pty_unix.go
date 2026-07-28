//go:build mew && !windows

package trinkets

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/phroun/mew"
	"github.com/phroun/purfecterm"
)

// hostPTY starts one command in a real PTY and hands back the session mew
// holds. This is the POSIX side: purfecterm's forkpty is exactly right, and
// its PTY type is mew's PTYSession plus Start — so it satisfies the narrower
// interface directly, and mew, holding only that interface, has no way to
// call Start on it.
//
// The Windows side is a separate file for a reason worth stating: there is no
// forkpty there, and a pseudoconsole is not a file descriptor you hand to a
// child but a handle you have to bind to it before it starts. See
// editor_mew_pty_windows.go.
func hostPTY(path, dir string, env []string, cols, rows int) (mew.PTYSession, error) {
	s, err := newUnixPTY(path, nil, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// unixPTY wraps purfecterm's PTY with the two things a diagnostic report
// needs and a plain session does not carry: how the child ended, and what
// happened on the way up.
type unixPTY struct {
	purfecterm.PTY
	cmd   *exec.Cmd
	trace []string

	// reaped closes when Wait has returned, so ExitStatus can tell a child
	// that exited from one still running. The two events race: the master fd
	// reports EOF the instant the child dies, which is BEFORE the reaper has
	// run, so a status read at that moment would report "still running" about
	// a process that has just ended.
	reaped chan struct{}
	mu     sync.Mutex
	code   int
	exited bool
}

func newUnixPTY(path string, args []string, dir string, env []string, cols, rows int) (*unixPTY, error) {
	p := &unixPTY{reaped: make(chan struct{})}
	pty, err := purfecterm.NewPTY()
	if err != nil {
		p.note("purfecterm.NewPTY: " + err.Error())
		return p, err
	}
	p.PTY = pty
	p.note("purfecterm.NewPTY: ok (forkpty)")

	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if err := pty.Start(cmd); err != nil {
		p.note("Start: " + err.Error())
		_ = pty.Close()
		return p, err
	}
	p.cmd = cmd
	p.note(fmt.Sprintf("Start: ok, pid %d", cmd.Process.Pid))
	if cols > 0 && rows > 0 {
		if err := pty.Resize(cols, rows); err != nil {
			p.note("Resize: " + err.Error())
		}
	}

	// Reaping is what turns "the child is gone" into a status anyone can
	// read; without it the exit code is only ever visible to the kernel.
	go func() {
		defer close(p.reaped)
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		if cmd.ProcessState != nil {
			p.code = cmd.ProcessState.ExitCode()
		} else if err != nil {
			p.code = -1
		}
		p.mu.Unlock()
	}()
	return p, nil
}

func (p *unixPTY) note(s string) { p.trace = append(p.trace, s) }

// ExitStatus implements mew.PTYExitStatus: it tells a child that ran and
// exited from a stream that ended under one still running.
//
// It allows the reaper a moment first. This is asked exactly once, as a
// session ends, and it is asked at the instant the two events race: the
// master fd goes to EOF the moment the child dies, which is before Wait has
// returned, so an immediate read would report every normal exit as a child
// still running. A brief wait at teardown buys an answer that is true.
func (p *unixPTY) ExitStatus() (int, bool) {
	if p.reaped != nil {
		select {
		case <-p.reaped:
		case <-time.After(reapGrace):
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, p.exited
}

// reapGrace is how long ExitStatus waits for Wait to land. Long enough for a
// process that has already died, short enough that a session ending is not a
// pause anyone notices.
const reapGrace = 200 * time.Millisecond

// Diagnostics is the account of how this session was built, for pty_diag.
func (p *unixPTY) Diagnostics() []string { return p.trace }

// diagShellNames are the shells worth looking for on this platform.
func diagShellNames() []string {
	return []string{"bash", "sh", "zsh", "fish"}
}

// hostPTYProbe runs the POSIX self-test: one session that should say
// something immediately, through the same code exec uses.
func hostPTYProbe() []probeResult {
	dir, _ := os.UserHomeDir()
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	shell, err := exec.LookPath("sh")
	if err != nil {
		return []probeResult{{Label: "sh -c echo", Err: err}}
	}
	var out []probeResult
	s, err := newUnixPTY(shell, []string{"-c", "echo MEW-PTY-OK"}, dir, env, 80, 24)
	out = append(out, runProbe("sh -c 'echo MEW-PTY-OK' (a child that speaks and exits)", s, err))

	s2, err2 := newUnixPTY(shell, nil, dir, env, 80, 24)
	out = append(out, runProbe("sh (interactive, the shape exec uses)", s2, err2))
	return out
}
