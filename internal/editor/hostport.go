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
	mu   sync.Mutex
	post func(fn func()) bool
	exec func(cmd string)
}

// bind attaches the port to a session. Called once at editor construction;
// Execute before bind (or when the input source cannot post) reports false.
func (p *HostPort) bind(post func(fn func()) bool, exec func(cmd string)) {
	p.mu.Lock()
	p.post, p.exec = post, exec
	p.mu.Unlock()
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
