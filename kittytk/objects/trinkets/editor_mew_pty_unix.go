//go:build mew && !windows

package trinkets

import (
	"os"
	"os/exec"

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
	pty, err := purfecterm.NewPTY()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path)
	cmd.Dir = dir
	cmd.Env = env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if err := pty.Start(cmd); err != nil {
		_ = pty.Close()
		return nil, err
	}
	if cols > 0 && rows > 0 {
		_ = pty.Resize(cols, rows)
	}
	return pty, nil
}
