//go:build mew

package trinkets

import (
	"os/exec"
	"sync"

	"github.com/phroun/mew"
)

// filterSession is the pipe flavor of a mew session: the child runs on ordinary
// pipes rather than a pseudo-terminal, so a command can be used as a FILTER —
// stdin fed from the editor, stdout and stderr read back SEPARATELY. It rides the
// same PTYProvider/PTYSession surface as the terminal flavor and adds the
// capabilities that only pipes can offer, through the optional interfaces mew
// type-asserts for:
//
//   - Read        → the child's stdout
//   - ReadStderr  → the child's stderr, on its own pipe (PTYStderr)
//   - Write       → the child's stdin
//   - CloseStdin  → half-close stdin to signal EOF so a filter finishes (PTYStdinCloser)
//   - ExitStatus  → the child's exit code once it has been waited on (PTYExitStatus)
//   - Resize      → a no-op: a pipe child has no terminal to resize
//   - Close       → full teardown (kill if still running)
//
// There is no pty here and no purfecterm: the bytes are exactly what the child
// wrote, which is the whole point of a filter. mew's own security argument is
// unchanged — process creation still happens only here, in the host.
type filterSession struct {
	cmd    *exec.Cmd
	stdin  interface{ Write([]byte) (int, error); Close() error }
	stdout interface{ Read([]byte) (int, error) }
	stderr interface{ Read([]byte) (int, error) }

	mu     sync.Mutex
	waited bool
	code   int
	exited bool
}

// newFilterSession starts path with three separate pipes for the child's
// standard streams. env and args are handed to the process verbatim, exactly as
// the terminal path does; dir is its working directory.
func newFilterSession(path, dir string, env, args []string) (mew.PTYSession, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &filterSession{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (f *filterSession) Read(p []byte) (int, error)       { return f.stdout.Read(p) }
func (f *filterSession) ReadStderr(p []byte) (int, error) { return f.stderr.Read(p) }
func (f *filterSession) Write(p []byte) (int, error)      { return f.stdin.Write(p) }

// CloseStdin half-closes the child's input, which is how a filter that reads to
// EOF is told there is no more input and may finish. It is NOT teardown: the
// child keeps running and writing until it exits on its own.
func (f *filterSession) CloseStdin() error { return f.stdin.Close() }

// Resize is a no-op: a pipe child has no terminal geometry.
func (f *filterSession) Resize(cols, rows int) error { return nil }

// ExitStatus waits for the child (once) and reports its exit code. It must be
// called only after stdout and stderr have been drained — exec.Cmd.Wait closes
// the pipes — which is exactly what the filter orchestration does at the end.
func (f *filterSession) ExitStatus() (int, bool) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.code, f.exited
}

// Close is the full teardown: kill the child if it is still running and reap it,
// so no process outlives the editor that asked for it. Closing stdin as well
// unblocks a child parked on a read.
func (f *filterSession) Close() error {
	if f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
	if f.stdin != nil {
		_ = f.stdin.Close()
	}
	f.wait()
	return nil
}

// wait reaps the child exactly once and records its exit code.
func (f *filterSession) wait() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waited {
		return
	}
	f.waited = true
	_ = f.cmd.Wait()
	if f.cmd.ProcessState != nil {
		f.code = f.cmd.ProcessState.ExitCode()
		f.exited = true
	}
}
