// Package client is the app-side veneer over the display protocol:
// typed handles with synchronous-looking reads served from an
// app-side replica, writes as fire-and-forget protocol statements,
// and event subscriptions folded into the replica before app
// handlers run (the slice-4 veneer contract, docs/d2-read-audit.md).
//
// A Conn is instance-scoped, never global (multi-display guardrail):
// one app may hold any number of connections. The package imports
// ONLY the protocol package - it compiles with no knowledge of the
// rendering side. In-process, the display side is the registered
// trinket vocabulary reached through protocol.RegistryFactory; under
// transport the same Conn speaks to a socket instead.
package client

import (
	"fmt"
	"sync"

	"github.com/phroun/kittytk/protocol"
)

// transport is how statements reach the display service: in-process
// session execution, or a socket carrying protocol text (D22).
type transport interface {
	exec(src string) (*protocol.Reply, error)
	close() error
}

// Conn is one connection to one display service.
type Conn struct {
	mu        sync.Mutex
	transport transport

	// Replica: per-object state folded from writes (class A) and
	// subscribed events (class B).
	types map[uint64]string
	state map[uint64]*objState

	// In-process escape hatch: the real constructed targets, keyed
	// by object ID. Nil entries under a future remote transport.
	targets map[uint64]any

	// App handlers by (object, event type) and by event type.
	handlers     map[uint64]map[string][]func(*protocol.Event)
	typeHandlers map[string][]func(*protocol.Event)

	// Subscriptions already sent (avoid duplicate sub statements).
	subs map[subKey]bool

	// Command sink for action= dispatch (the app's registry).
	dispatch func(commandID string)

	// appID is this connection's Application ObjectID, reported by the
	// display service in the handshake (0 for in-process connections, which
	// have no handshake). Address app-wide properties by it - see SetApp.
	appID uint64

	// closed fires once when the transport disconnects (remote) or
	// Close is called, so callers can block on the connection's life.
	closed    chan struct{}
	closeOnce sync.Once
}

type subKey struct {
	id    uint64
	event string
}

// objState is the replica of one object's class-B state.
type objState struct {
	checked  protocol.FlagState
	text     string
	selected int
	result   string
}

func newConn(dispatch func(commandID string)) *Conn {
	return &Conn{
		types:        make(map[uint64]string),
		state:        make(map[uint64]*objState),
		targets:      make(map[uint64]any),
		handlers:     make(map[uint64]map[string][]func(*protocol.Event)),
		typeHandlers: make(map[string][]func(*protocol.Event)),
		subs:         make(map[subKey]bool),
		dispatch:     dispatch,
		closed:       make(chan struct{}),
	}
}

// Closed returns a channel that is closed when the connection ends,
// whether by the app calling Close or the display service disconnecting.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// AppID returns the ObjectID of this connection's application, as reported by
// the display service in the handshake. It is 0 for in-process connections
// (which have no handshake). Use it to address application-wide properties -
// e.g. c.Exec(fmt.Sprintf("set %d multiwindow", c.AppID())), or SetApp.
func (c *Conn) AppID() uint64 { return c.appID }

// SetApp applies application-wide properties to this connection's app with the
// same syntax as any object: SetApp("multiwindow contextonly") sends
// `set <appID> multiwindow contextonly`. It errors before the handshake has
// assigned an app ID (in-process connections have none).
func (c *Conn) SetApp(props string) (*protocol.Reply, error) {
	if c.appID == 0 {
		return nil, fmt.Errorf("SetApp: no application id (in-process connection)")
	}
	return c.Exec(fmt.Sprintf("set %d %s", c.appID, props))
}

// markClosed fires the Closed channel exactly once.
func (c *Conn) markClosed() { c.closeOnce.Do(func() { close(c.closed) }) }

// NewInProcess creates a connection whose display side is the
// registered trinket vocabulary in this process. dispatch receives
// action= command IDs (pass the application registry's Dispatch;
// nil is allowed for connections that use no commands).
func NewInProcess(dispatch func(commandID string)) *Conn {
	c := newConn(dispatch)
	// Commands arrive uniformly as command events (deliver invokes
	// the dispatch sink), so the BindContext dispatch stays nil -
	// FireAction still emits the event, and there is exactly one
	// dispatch path in-process and remote alike.
	ctx := &protocol.BindContext{
		Emit: c.deliver,
	}
	factory := &recordingFactory{conn: c, inner: protocol.NewRegistryFactory(ctx)}
	c.transport = &inProcessTransport{
		session: protocol.NewSession(),
		factory: factory,
	}
	return c
}

// inProcessTransport executes against the local session/factory.
type inProcessTransport struct {
	session *protocol.Session
	factory protocol.Factory
}

func (t *inProcessTransport) exec(src string) (*protocol.Reply, error) {
	script, err := protocol.Parse(src)
	if err != nil {
		return nil, err
	}
	return t.session.Execute(script, t.factory)
}

func (t *inProcessTransport) close() error { return nil }

// recordingFactory interposes on construction to record each object's
// type and (in-process) target into the replica tables.
type recordingFactory struct {
	conn  *Conn
	inner protocol.Factory
}

func (f *recordingFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	f.conn.mu.Lock()
	f.conn.types[o.ID()] = typeName
	if tg, ok := o.(interface{ Target() any }); ok {
		f.conn.targets[o.ID()] = tg.Target()
	}
	f.conn.mu.Unlock()
	return o, nil
}

// Forward EventControl to the inner factory (wrappers must not hide
// the capability).
func (f *recordingFactory) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *recordingFactory) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *recordingFactory) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}

// Exec executes protocol text on this connection (one batch; the
// remote transport appends the D22 end terminator).
func (c *Conn) Exec(src string) (*protocol.Reply, error) {
	return c.transport.exec(src)
}

// Describe queries the host's wire vocabulary (D24): the supported
// trinket types and, for each, the properties it accepts with each
// property's kind, default, and a brief description. Common properties
// (accepted by every non-virtual type) are reported once.
func (c *Conn) Describe() (*protocol.Vocabulary, error) {
	reply, err := c.transport.exec("describe")
	if err != nil {
		return nil, err
	}
	return protocol.DecodeVocabulary(reply.Extra)
}

// Close releases the connection (closes the socket for remote
// connections; no-op in-process).
func (c *Conn) Close() error {
	err := c.transport.close()
	c.markClosed()
	return err
}

// Build executes a construction script and returns handle access to
// its surfaced names.
func (c *Conn) Build(src string) (*UI, error) {
	reply, err := c.Exec(src)
	if err != nil {
		return nil, err
	}
	return &UI{conn: c, ids: reply.IDs}, nil
}

// deliver folds an event into the replica, then invokes handlers.
// (BindContext already filtered by subscription and suppression.)
func (c *Conn) deliver(ev *protocol.Event) {
	id, _ := ev.Trinket()

	c.mu.Lock()
	st := c.state[id]
	if st == nil {
		st = &objState{selected: -1}
		c.state[id] = st
	}
	var dispatchAction string
	switch ev.Type {
	case "toggle":
		st.checked = ev.Flag("checked")
	case "change":
		if s, ok := ev.Text("text"); ok {
			st.text = s
		}
		if n, ok := ev.Int("selected"); ok {
			st.selected = n
		}
	case "finish":
		if w, ok := ev.Word("result"); ok {
			st.result = w
		}
	case "command":
		// The one dispatch path (in-process and remote): command
		// events invoke the app's dispatch sink.
		if a, ok := ev.Word("action"); ok {
			dispatchAction = a
		}
	}
	var fns []func(*protocol.Event)
	if hs, ok := c.handlers[id]; ok {
		fns = append(fns, hs[ev.Type]...)
	}
	fns = append(fns, c.typeHandlers[ev.Type]...)
	dispatch := c.dispatch
	c.mu.Unlock()

	if dispatchAction != "" && dispatch != nil {
		dispatch(dispatchAction)
	}
	for _, fn := range fns {
		fn(ev)
	}
}

// ensureSub sends a sub statement once per (object, event).
func (c *Conn) ensureSub(id uint64, event string) {
	c.mu.Lock()
	key := subKey{id, event}
	if c.subs[key] {
		c.mu.Unlock()
		return
	}
	c.subs[key] = true
	c.mu.Unlock()
	// Errors here indicate a connection without event support; the
	// replica then simply never updates - surfaced by tests, not
	// silent corruption of app state.
	_, _ = c.Exec(fmt.Sprintf("sub %d %s", id, event))
}

// on registers an app handler and opens the event flow.
func (c *Conn) on(id uint64, event string, fn func(*protocol.Event)) {
	c.ensureSub(id, event)
	c.mu.Lock()
	if c.handlers[id] == nil {
		c.handlers[id] = make(map[string][]func(*protocol.Event))
	}
	c.handlers[id][event] = append(c.handlers[id][event], fn)
	c.mu.Unlock()
}

// OnCommand registers a handler for command events carrying the given
// action ID (command events flow unconditionally per D20). This is
// event observation; the authoritative dispatch is the sink passed to
// NewInProcess.
func (c *Conn) OnCommand(action string, fn func()) {
	c.mu.Lock()
	c.typeHandlers["command"] = append(c.typeHandlers["command"], func(ev *protocol.Event) {
		if a, ok := ev.Word("action"); ok && a == action {
			fn()
		}
	})
	c.mu.Unlock()
}

// stateOf returns the replica entry, creating it lazily.
func (c *Conn) stateOf(id uint64) *objState {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.state[id]
	if st == nil {
		st = &objState{selected: -1}
		c.state[id] = st
	}
	return st
}

// set sends a set statement for one object (fire-and-forget; D20
// guarantees it will not echo back).
func (c *Conn) set(id uint64, args string) error {
	_, err := c.Exec(fmt.Sprintf("set %d %s", id, args))
	return err
}

// UI is handle access to one Build's surfaced names.
type UI struct {
	conn *Conn
	ids  map[string]uint64
}

// ID returns the ObjectID behind a surfaced name (0 if absent).
func (u *UI) ID(name string) uint64 { return u.ids[name] }

// Has reports whether a name was surfaced.
func (u *UI) Has(name string) bool { _, ok := u.ids[name]; return ok }
