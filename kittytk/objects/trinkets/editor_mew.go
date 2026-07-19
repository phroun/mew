//go:build mew

// Package trinkets: the mew-backed Editor trinket.
//
// Build-tagged `mew` because it pulls in the mew editor module
// (github.com/phroun/mew). The default build and the standard test gate
// never compile this file; build with `-tags mew` (and `go get
// github.com/phroun/mew@0.3.1-alpha` first, if it is not yet in go.mod).
package trinkets

import (
	"io"
	"sync"

	"github.com/phroun/mew"
)

// Editor is a full text-editor trinket backed by the mew editor library,
// displayed through an embedded PurfecTerm running as an editor surface
// (scrollback disabled, no scrollbar lane).
//
// The wiring is deliberately symmetric and free of any key-name
// translation: mew renders a terminal escape stream, which flows into
// PurfecTerm.Feed (the display direction); PurfecTerm encodes the user's
// keystrokes/mouse into raw terminal input bytes, which flow back out
// through its input sink into mew's input reader. The PurfecTerm is a
// pure display+input surface here, exactly as it is when driving a
// remote PTY - so mew's own input parser and display renderer do all the
// work, and this trinket is just the plumbing plus lifecycle.
//
// Concept-proving baseline: how host files and paths actually reach mew
// - resolved against the KittyTK host OS server-side, or transported
// over the protocol to the client - is left entirely to the
// mew.FileSystem the caller supplies (EditorOptions.FileSystem). That
// seam is where the file/path story gets worked out once this baseline
// runs; nothing here bakes in a policy.
//
// Editor embeds *PurfecTerm, inheriting its full trinket behavior
// (paint, focus, input routing, editor mode). It adds only the mew
// session lifecycle. NOTE (self-dispatch): because the embedded
// PurfecTerm registered itself as the framework "self" at construction,
// focus/layout resolve to the PurfecTerm - which is exactly what we
// want, since all display and input semantics are the PurfecTerm's. The
// host must call Editor.Close (not PurfecTerm.Close) to also stop the
// mew session.
type Editor struct {
	*PurfecTerm

	opts EditorOptions

	// input direction: PurfecTerm keystrokes -> inPipeW; mew reads inPipeR.
	inPipeR *io.PipeReader
	inPipeW *io.PipeWriter

	// resizeCh signals mew that the grid size changed; it re-reads Size().
	resizeCh chan struct{}

	mu          sync.Mutex
	curCols     int
	curRows     int
	inputClosed bool

	done chan struct{}
	err  error
}

// EditorOptions configures a mew-backed Editor.
type EditorOptions struct {
	// Content is the initial text when Filename is empty (mew.EditContent).
	Content string

	// Filename, when set, opens that path through mew (mew.EditFile),
	// resolved via FileSystem.
	Filename string

	// FileSystem virtualizes document I/O. nil uses mew.OSFileSystem()
	// (the real local disk - the KittyTK host OS, server-side). Supplying
	// a custom FileSystem is the seam for transporting file access over
	// the KittyTK protocol to the client later.
	FileSystem mew.FileSystem

	// MewFileSystem virtualizes mew's own config/profile/lock storage
	// (the mew:/ scheme). nil uses OSFileSystem().
	MewFileSystem mew.FileSystem

	// ColdStoragePath is where mew/garland spill large buffers. Empty
	// lets mew decide.
	ColdStoragePath string

	// ConfigText, if set, is mew configuration applied to this session.
	ConfigText string

	// Identity stamps the editor session's user/host/pid (lock files,
	// LockOwner). Leave all zero to use mew's environment default.
	IdentityUser string
	IdentityHost string
	IdentityPID  int

	// OnExit, if set, is called (on the session goroutine) when the mew
	// session ends - so the host can close the containing window, mark
	// the tab, etc. err is mew's return (nil on a clean quit).
	OnExit func(err error)
}

// NewEditor creates a mew-backed editor trinket and starts its session
// on a background goroutine. Place it in a window like any trinket; call
// Close to end the session and release resources.
func NewEditor(opts EditorOptions) *Editor {
	e := &Editor{
		PurfecTerm: NewPurfecTerm(),
		opts:       opts,
		resizeCh:   make(chan struct{}, 1),
		done:       make(chan struct{}),
		curCols:    80,
		curRows:    24,
	}
	e.SetEditorMode(true)

	if e.Terminal() == nil {
		// The terminal surface failed to initialize; there is nothing to
		// render into, so do not start a session.
		close(e.done)
		return e
	}

	e.inPipeR, e.inPipeW = io.Pipe()

	// Keystrokes/mouse the PurfecTerm encodes become mew's input. If mew
	// has already exited its read side, the write errors and is dropped.
	e.SetInputSink(func(b []byte) {
		e.mu.Lock()
		closed := e.inputClosed
		e.mu.Unlock()
		if closed {
			return
		}
		_, _ = e.inPipeW.Write(b)
	})

	// Grid-size changes wake mew's resize path (which re-reads Size()).
	// SetResizeSink fires once immediately with the current size.
	e.SetResizeSink(func(cols, rows int) {
		e.mu.Lock()
		e.curCols, e.curRows = cols, rows
		e.mu.Unlock()
		select {
		case e.resizeCh <- struct{}{}:
		default: // a pending signal already covers this change
		}
	})

	go e.run()
	return e
}

// writerFunc adapts a function to io.Writer (mew's display sink).
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func (e *Editor) run() {
	defer close(e.done)

	term := mew.Terminal{
		// Display: mew's escape stream parsed straight into the terminal
		// buffer. Feed is safe to call off the paint goroutine (it is the
		// same sink the remote-PTY wire feeds).
		Output: writerFunc(func(p []byte) (int, error) {
			e.Feed(p)
			return len(p), nil
		}),
		// Input: raw terminal bytes the PurfecTerm produced from user input.
		Input: e.inPipeR,
		// Size: the current grid, in cells.
		Size: func() (int, int, error) {
			e.mu.Lock()
			c, r := e.curCols, e.curRows
			e.mu.Unlock()
			return c, r, nil
		},
		Resize: e.resizeCh,
	}

	fs := e.opts.FileSystem
	if fs == nil {
		fs = mew.OSFileSystem()
	}
	options := []mew.Option{
		mew.WithTerminal(term),
		mew.WithFileSystem(fs),
	}
	if e.opts.MewFileSystem != nil {
		options = append(options, mew.WithMewFileSystem(e.opts.MewFileSystem))
	}
	if e.opts.ColdStoragePath != "" {
		options = append(options, mew.WithColdStoragePath(e.opts.ColdStoragePath))
	}
	if e.opts.ConfigText != "" {
		options = append(options, mew.WithConfigText(e.opts.ConfigText))
	}
	if e.opts.IdentityUser != "" || e.opts.IdentityHost != "" || e.opts.IdentityPID != 0 {
		options = append(options,
			mew.WithIdentity(e.opts.IdentityUser, e.opts.IdentityHost, e.opts.IdentityPID))
	}

	var err error
	if e.opts.Filename != "" {
		err = mew.EditFile(e.opts.Filename, options...)
	} else {
		_, err = mew.EditContent(e.opts.Content, options...)
	}
	e.err = err

	if e.opts.OnExit != nil {
		e.opts.OnExit(err)
	}
}

// Close ends the mew session (by closing its input, which mew sees as
// EOF and exits on) and releases the terminal surface. Idempotent.
func (e *Editor) Close() {
	e.mu.Lock()
	if !e.inputClosed {
		e.inputClosed = true
		if e.inPipeW != nil {
			e.inPipeW.Close()
		}
	}
	e.mu.Unlock()

	if e.PurfecTerm != nil {
		e.PurfecTerm.Close()
	}
}

// Wait blocks until the mew session has ended.
func (e *Editor) Wait() { <-e.done }

// Err blocks until the session ends and returns mew's result (nil on a
// clean quit).
func (e *Editor) Err() error {
	<-e.done
	return e.err
}
