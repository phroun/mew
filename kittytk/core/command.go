// Package core provides fundamental types for KittyTK.
package core

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// CommandRegistry maps stable command IDs to handlers. It is the
// dispatch seam of the display protocol (D2): UI objects trigger by
// ID at a single boundary instead of invoking closures they hold.
// In-process it is a direct map; under the protocol the display
// service emits "command triggered" events carrying the same IDs and
// the app-side client library dispatches through a registry like this.
type CommandRegistry struct {
	mu       sync.RWMutex
	handlers map[string]func()
}

// NewCommandRegistry creates an empty command registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{handlers: make(map[string]func())}
}

// Register sets the handler for a command ID, replacing any previous.
func (r *CommandRegistry) Register(id string, handler func()) {
	if id == "" || handler == nil {
		return
	}
	r.mu.Lock()
	r.handlers[id] = handler
	r.mu.Unlock()
}

// Unregister removes the handler for a command ID.
func (r *CommandRegistry) Unregister(id string) {
	r.mu.Lock()
	delete(r.handlers, id)
	r.mu.Unlock()
}

// Has reports whether a handler is registered for the ID.
func (r *CommandRegistry) Has(id string) bool {
	r.mu.RLock()
	_, ok := r.handlers[id]
	r.mu.RUnlock()
	return ok
}

// Dispatch invokes the handler for the ID. Returns true if one ran.
func (r *CommandRegistry) Dispatch(id string) bool {
	r.mu.RLock()
	handler := r.handlers[id]
	r.mu.RUnlock()
	if handler == nil {
		return false
	}
	handler()
	return true
}

var autoCommandCounter atomic.Uint64

// NextAutoCommandID returns a process-unique fallback command ID for
// items not given a semantic ID (e.g. "file.open" — see
// StandardActions). Semantic IDs are preferred: they are stable across
// runs, which matters once IDs travel over the display protocol.
func NextAutoCommandID() string {
	return fmt.Sprintf("cmd.auto.%d", autoCommandCounter.Add(1))
}
