package core

import (
	"sync"

	"github.com/phroun/key-sequence-processor/keyseq"
)

// A KeyRegistry is a key-to-command table: a key or key sequence as the input
// layer reports it, mapped to the name of the command it runs. It is the whole
// of what a keymap is — there is one active at a time, "default" unless a
// scope overrides it, and the toolkit's own bindings are entries in it like
// anyone else's.
//
// A registry is not a dispatch mechanism. It answers what a key MEANS; who
// acts on that meaning is the existing chain's business, unchanged.
type KeyRegistry struct {
	mu       sync.RWMutex
	name     string
	bindings map[string]string
	revision uint64
}

// NewKeyRegistry creates a registry from a key-to-command table. The name is
// for diagnostics — "default", "purfecterm-captured" — and has no behaviour.
func NewKeyRegistry(name string, bindings map[string]string) *KeyRegistry {
	r := &KeyRegistry{name: name, bindings: make(map[string]string, len(bindings))}
	for k, v := range bindings {
		r.bindings[k] = v
	}
	return r
}

// Name returns the registry's name.
func (r *KeyRegistry) Name() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.name
}

// Revision is bumped by every edit. A context built from this registry records
// the revision it was built at, so staleness is a comparison rather than a
// subscription — nothing has to remember to notify anybody.
func (r *KeyRegistry) Revision() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revision
}

// Bind sets one binding. An empty command unbinds the key, which is how a user
// turns a default off without having to know what it was.
func (r *KeyRegistry) Bind(key, command string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if command == "" {
		delete(r.bindings, key)
	} else {
		r.bindings[key] = command
	}
	r.revision++
}

// KeysFor returns every key bound to a command, in no particular order. A
// command may have several keys, or none — the coarse window move is bound to
// Ctrl, Meta and Super arrows alike, and a command nothing names is simply not
// reachable from the keyboard.
func (r *KeyRegistry) KeysFor(command string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var keys []string
	for k, c := range r.bindings {
		if c == command {
			keys = append(keys, k)
		}
	}
	return keys
}

// A KeyContext is the set of actions available right now, ready to resolve a
// key against. There is one at a time per scope: not a slice per widget, but
// everything the current situation offers.
//
// It is stateful, because a sequence is: feeding it the first key of a chord
// holds that prefix until the chord completes or is abandoned.
type KeyContext struct {
	mu       sync.Mutex
	proc     *keyseq.Processor
	commands map[string]bool
	revision uint64
}

// BuildContext derives a context from the registry, carrying only the bindings
// whose command is in commands. Everything else in the registry is invisible
// here — which is the point: a key bound to something this situation cannot do
// should not resolve to it.
//
// The processor runs in resolution-only mode, so nothing is dispatched.
// Resolve reports the command and the caller acts, exactly as the switch it
// replaces did.
func (r *KeyRegistry) BuildContext(commands []string) *KeyContext {
	set := make(map[string]bool, len(commands))
	for _, c := range commands {
		set[c] = true
	}
	ctx := &KeyContext{
		proc:     keyseq.NewProcessor(nil),
		commands: set,
		revision: r.Revision(),
	}
	mappings := map[string]string{}
	if r != nil {
		r.mu.RLock()
		for k, c := range r.bindings {
			if set[c] {
				mappings[k] = c
			}
		}
		r.mu.RUnlock()
	}
	ctx.proc.SetMappings(mappings)
	return ctx
}

// Revision reports the registry revision this context was built at.
func (c *KeyContext) Revision() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revision
}

// Add binds a key to a command inside an already-built context, for entries
// that are formed rather than configured — the menu accelerators, whose keys
// depend on the menu titles rather than on the registry.
func (c *KeyContext) Add(key, command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands[command] = true
	m := c.proc.GetAllMappings()
	m[key] = command
	c.proc.SetMappings(m)
}

// Claims reports whether a key or sequence already resolves to something here.
// This is what a menu accelerator asks before forming: a chord something else
// has claimed is not the accelerator's to take.
func (c *KeyContext) Claims(key string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.proc.GetAllMappings() {
		if k == key {
			return true
		}
	}
	return false
}

// Resolve reports the command a key means here, "" for none. A key that opens
// a longer sequence resolves to nothing yet and the prefix is held, which is
// why the context is stateful.
func (c *KeyContext) Resolve(key string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.proc.ProcessKey(key).Command
}

// Abandon drops a partly-typed sequence.
func (c *KeyContext) Abandon() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proc.ClearActiveSequence()
}
