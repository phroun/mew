package editor

import (
	"sync"

	"github.com/phroun/mew/internal/input"
)

// HostPort lets an embedding host inject editor commands into a running
// session from its own threads. The host creates one, hands it in via
// Config.HostPort, and calls Execute whenever one of its UI affordances (an
// Edit-menu item, a context-menu item) should run a mew command. Each
// Execute is marshaled onto the editor's main loop — through the input
// source's action queue — so it runs with exactly the safety of a keystroke.
type HostPort struct {
	mu    sync.Mutex
	post  func(fn func()) bool
	exec  func(cmd string)
	keys  func(action, preferred string) string
	opt   func(name string) string
	paste func(text string) bool
}

// bind attaches the port to a session. Called once at editor construction;
// Execute before bind (or when the input source cannot post) reports false.
func (p *HostPort) bind(post func(fn func()) bool, exec func(cmd string), keys func(action, preferred string) string, opt func(name string) string, paste func(text string) bool) {
	p.mu.Lock()
	p.post, p.exec, p.keys, p.opt, p.paste = post, exec, keys, opt, paste
	p.mu.Unlock()
}

// Paste inserts text the HOST received as a paste — a bracketed paste its
// terminal reported, or a drop onto its window — into the focused viewport.
//
// Not Execute with a command built around the text: this goes down the editor's
// own paste path, so it arrives with everything a paste is entitled to. Line
// endings are normalised, the read-only gate is checked at the point of
// insertion, and the whole of it collapses into ONE undo revision however many
// chunks it came in — which a series of commands could not do, since each would
// close its own.
//
// Delivered in order with the key stream, so text pasted between two keystrokes
// lands between them. Reports false when the port is not bound to a session or
// the input source cannot accept a paste.
func (p *HostPort) Paste(text string) bool {
	p.mu.Lock()
	paste := p.paste
	p.mu.Unlock()
	if paste == nil || text == "" {
		return false
	}
	return paste(text)
}

// Option reads an option's current effective value through the same cascade
// get_option uses, for a host that wants to REFLECT editor state in its own UI
// — a menu item's checkmark, or a caption that names the value it will change.
//
// Like KeyBinding this is a synchronous READ, safe from any goroutine, and
// empty before the port is bound to a session (and for an unknown name).
func (p *HostPort) Option(name string) string {
	p.mu.Lock()
	opt := p.opt
	p.mu.Unlock()
	if opt == nil {
		return ""
	}
	return opt(name)
}

// KeyBinding resolves the key a mew command is bound to, for a host that wants
// to ADVERTISE a mew binding in its own UI — a menu item's shortcut column,
// say, for a key mew handles and the toolkit never sees. It answers exactly
// what the %keys#action|preferred% modebar code would: the live keymap's best
// match for action, choosing among several bindings by how closely each
// resembles preferred, and falling back to preferred itself when nothing is
// bound.
//
// Unlike Execute this is a synchronous READ, safe from any goroutine: it takes
// the same lock the editor holds while rewriting the keymap. Empty before the
// port is bound to a session.
func (p *HostPort) KeyBinding(action, preferred string) string {
	p.mu.Lock()
	keys := p.keys
	p.mu.Unlock()
	if keys == nil {
		return ""
	}
	return keys(action, preferred)
}

// Execute queues a mew command (e.g. "os_copy") to run on the editor's main
// loop. Safe from any goroutine. Reports false when the port is not bound to
// a session or the session's input source cannot accept posted actions.
func (p *HostPort) Execute(cmd string) bool {
	p.mu.Lock()
	post, exec := p.post, p.exec
	p.mu.Unlock()
	if post == nil || exec == nil || cmd == "" {
		return false
	}
	return post(func() { exec(cmd) })
}

// hostPaste feeds text a host received as a paste into the input source, as one
// final chunk. Backs HostPort.Paste; see there for what the paste path gives it
// that a command could not.
//
// Only a source that can carry a paste answers: the terminal-backed one reads
// its own bracketed paste off the wire and has no need of this, and a source
// that cannot take one reports false rather than dropping the text silently.
func (e *Editor) hostPaste(text string) bool {
	pf, ok := e.KeyHandler.(interface {
		SendPaste(content []byte, final bool) bool
	})
	if !ok {
		return false
	}
	return pf.SendPaste([]byte(text), true)
}

// PostAction queues fn to run on the editor's main loop, when the input
// source supports posting (both the terminal-backed source and the host
// EventFeed do). It is the marshal for asynchronous host callbacks — a
// clipboard read resolving on a UI thread, say — and reports false when the
// source cannot deliver.
func (e *Editor) PostAction(fn func()) bool {
	if ap, ok := e.KeyHandler.(input.ActionPoster); ok {
		return ap.PostAction(fn)
	}
	return false
}
