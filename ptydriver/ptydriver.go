// Package ptydriver runs a child process on the CLIENT side of the
// display protocol and bridges it to a terminal surface.
//
// Under KittyTK's network-rendering model the render server draws the
// terminal but never spawns the child: the process belongs to the
// application (the client). A Driver owns a real PTY, spawns the shell (or
// any command) into it, and pumps the child's output to a caller-supplied
// sink - typically the terminal's feed= property over the wire, or its
// Feed method in-process. The reverse direction (the user's keystrokes,
// mouse reports, and paste, plus grid-size changes) is delivered back to
// the Driver via Input and Resize, which it writes to the PTY.
package ptydriver

import (
	"os"
	"os/exec"
	"sync"

	"github.com/phroun/purfecterm"
)

// Driver bridges a client-side PTY to a terminal surface. Create one with
// Start; feed user input to Input/Resize; close it with Close.
type Driver struct {
	pty  purfecterm.PTY
	cmd  *exec.Cmd
	done chan struct{}
}

// defaultShell picks the user's shell, falling back to a POSIX sh.
func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// Start spawns command (empty name = the user's login shell) in a fresh
// PTY and begins pumping its output to feed. feed is called from a reader
// goroutine with each chunk the child writes; it must be safe to call
// concurrently with the caller's other work (the wire client and the
// in-process Feed both are). The child inherits the environment with
// TERM/COLORTERM advertising a modern terminal.
func Start(command string, feed func([]byte), args ...string) (*Driver, error) {
	pty, err := purfecterm.NewPTY()
	if err != nil {
		return nil, err
	}
	if command == "" {
		command = defaultShell()
	}
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	if err := pty.Start(cmd); err != nil {
		pty.Close()
		return nil, err
	}
	d := &Driver{pty: pty, cmd: cmd, done: make(chan struct{})}
	go d.readLoop(feed)
	return d, nil
}

// readBufSize is the pty read buffer per read syscall.
const readBufSize = 64 * 1024

// maxPending caps the coalescing buffer. Each feed is a synchronous wire
// round-trip that stalls until the host applies it and replies; a reader
// goroutine keeps draining the pty into `pending` meanwhile, so the next feed
// ships everything that piled up (one full-screen frame becomes one batch, no
// matter how the pty chunked its reads). Past this watermark the reader stops
// pulling from the pty - the child then flow-controls on a full pty buffer,
// bounding memory instead of racing ahead of what the display can absorb.
const maxPending = 8 << 20 // 8 MB

// readLoop pumps child output to feed until the PTY closes. It decouples
// reading from sending: a reader goroutine drains the pty into a shared buffer
// as fast as it can, while this loop ships the whole accumulated buffer per
// feed (round-trip) - coalescing bursty output into as few batches as the wire
// can carry.
func (d *Driver) readLoop(feed func([]byte)) {
	defer close(d.done)

	var (
		mu      sync.Mutex
		cond    = sync.NewCond(&mu)
		pending []byte
		closed  bool
	)

	// Reader: blocking reads append to pending; back off past the watermark.
	go func() {
		buf := make([]byte, readBufSize)
		for {
			mu.Lock()
			for len(pending) >= maxPending && !closed {
				cond.Wait()
			}
			stop := closed
			mu.Unlock()
			if stop {
				return
			}
			n, err := d.pty.Read(buf)
			if n > 0 {
				mu.Lock()
				pending = append(pending, buf[:n]...)
				cond.Broadcast()
				mu.Unlock()
			}
			if err != nil {
				mu.Lock()
				closed = true
				cond.Broadcast()
				mu.Unlock()
				return
			}
		}
	}()

	// Sender: take everything accumulated and ship it as one feed; while that
	// round-trip is in flight the reader keeps filling pending.
	for {
		mu.Lock()
		for len(pending) == 0 && !closed {
			cond.Wait()
		}
		chunk := pending
		pending = nil
		done := closed && len(chunk) == 0
		cond.Broadcast() // wake the reader if it was at the watermark
		mu.Unlock()

		if len(chunk) > 0 && feed != nil {
			feed(chunk)
		}
		if done {
			return
		}
	}
}

// Input writes bytes the user produced (keystrokes, mouse reports, paste)
// to the child process.
func (d *Driver) Input(b []byte) {
	if len(b) > 0 {
		_, _ = d.pty.Write(b)
	}
}

// Resize sets the child's PTY winsize to cols x rows.
func (d *Driver) Resize(cols, rows int) {
	if cols > 0 && rows > 0 {
		_ = d.pty.Resize(cols, rows)
	}
}

// Done returns a channel closed when the child's output stream ends (the
// process exited or the PTY was closed).
func (d *Driver) Done() <-chan struct{} { return d.done }

// Close terminates the child and releases the PTY.
func (d *Driver) Close() {
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
	}
	if d.pty != nil {
		_ = d.pty.Close()
	}
}
