// Package client is the app-side veneer over the display protocol:
// typed handles with synchronous-looking reads served from an
// app-side replica, writes as fire-and-forget protocol statements,
// and event subscriptions folded into the replica before app
// handlers run (the slice-4 veneer contract, docs/d2-read-audit.md).
//
// A Conn is instance-scoped, never global (multi-display guardrail):
// one app may hold any number of connections. The package imports the
// wire language and nothing else, so it compiles with no knowledge of
// the rendering side and none of the toolkit's dependencies: an app
// that only speaks the protocol carries only this.
//
// A remote Conn reaches a socket (remote.go). A Conn whose display side
// is the trinket vocabulary in this same process is built on the
// exported seam below by package inprocess, which is host code and
// lives outside this package for that reason.
package client

import (
	"fmt"
	"sort"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// Transport is how statements reach the display service: a socket
// carrying protocol text (D22), or a session executed in this process.
type Transport interface {
	Exec(src string) (*wire.Reply, error)
	Close() error
}

// Conn is one connection to one display service.
type Conn struct {
	mu        sync.Mutex
	transport Transport

	// Replica: per-object state folded from writes (class A) and
	// subscribed events (class B).
	types map[uint64]string
	state map[uint64]*objState

	// In-process escape hatch: the real constructed targets, keyed
	// by object ID. Nil entries under a future remote transport.
	targets map[uint64]any

	// App handlers by (object, event type) and by event type.
	handlers     map[uint64]map[string][]func(*wire.Event)
	typeHandlers map[string][]func(*wire.Event)

	// Subscriptions already sent (avoid duplicate sub statements).
	subs map[subKey]bool

	// Command sink for action= dispatch (the app's registry).
	dispatch func(commandID string)

	// What this application serves, and what the display is currently reading
	// of it. Statements arriving for one of these are the other direction of
	// the wire: the display asking, rather than being told (client/query.go).
	//
	// lastHostedID names them in this application's own space: each end mints
	// its own ids, and the direction a statement travelled says whose space it
	// is in, so the two never have to be told apart.
	sources      map[string]*Source
	queries      map[uint64]*Query
	lastHostedID uint64

	// given is every object the display has handed this connection, by the
	// name it knows it by: its application, its store, its handle on the
	// display, and whatever else a display offers. Empty for an in-process
	// connection, which has no handshake.
	//
	// One record rather than a field per object, so a display that hands over
	// something new reaches a client that was never taught its name -- and so
	// an init arriving later replaces what a name meant rather than leaving
	// two answers. Guarded by mu: the read loop writes it.
	given map[string]uint64

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
	checked  wire.FlagState
	text     string
	selected int
	result   string
}

// NewWithTransport builds a Conn over t. dispatch receives action=
// command IDs (nil is allowed for connections that use no commands).
//
// This is the seam a transport is written against: package inprocess
// uses it, and so does an application supplying a transport of its own.
// Applications talking to a display service want Dial instead.
func NewWithTransport(t Transport, dispatch func(commandID string)) *Conn {
	c := newConn(dispatch)
	c.transport = t
	return c
}

func newConn(dispatch func(commandID string)) *Conn {
	return &Conn{
		types:        make(map[uint64]string),
		state:        make(map[uint64]*objState),
		targets:      make(map[uint64]any),
		handlers:     make(map[uint64]map[string][]func(*wire.Event)),
		typeHandlers: make(map[string][]func(*wire.Event)),
		subs:         make(map[subKey]bool),
		sources:      make(map[string]*Source),
		queries:      make(map[uint64]*Query),
		dispatch:     dispatch,
		closed:       make(chan struct{}),
	}
}

// Closed returns a channel that is closed when the connection ends,
// whether by the app calling Close or the display service disconnecting.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// AppID returns the ObjectID of this connection's application, as reported by
// the display service in the handshake. It is 0 for in-process connections
// (which have no handshake). The display also knows the application by name,
// so `set app multiwindow` says the same thing as this id does; the id is
// what a client wants when it has taken the name for something else.
func (c *Conn) AppID() uint64 { return c.Init(wire.AppName) }

// App is this connection's application object as a handle: what app-wide
// properties are set through, and what it raises events on.
func (c *Conn) App() Handle { return c.Given(wire.AppName) }

// handOver records an init statement: every field of it is an object the
// display has handed this connection, under the name it knows it by. A name
// already in hand is replaced, because the display saying it again is the
// display saying what that name means NOW.
//
// It runs at the handshake and again whenever one arrives afterwards, so this
// is read from the connection's own goroutine as well as the caller's.
func (c *Conn) handOver(stmt *wire.Statement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.given == nil {
		c.given = map[string]uint64{}
	}
	for _, a := range stmt.Args {
		if a.Value == nil || a.Value.Kind != wire.NumberValue || !a.Value.IsInt {
			continue
		}
		c.given[a.Name] = uint64(a.Value.Int)
	}
}

// Init is the ObjectID of a thing the display handed this connection, by the
// name it knows it by -- "app", "store", "host", and whatever a display offers
// beyond them. 0 for a name it has not handed over.
//
// The name is also a session key the display bound, so `set <name> ...` says
// the same thing as this id does; this is what reaches the object when a
// client has taken that name for something of its own.
func (c *Conn) Init(name string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.given[name]
}

// InitNames is every name the display has handed over, sorted.
func (c *Conn) InitNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.given))
	for n := range c.given {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Given is a handle on one of them, by name.
func (c *Conn) Given(name string) Handle { return c.handed(c.Init(name), name) }

// HostID returns the ObjectID of this connection's handle on the display, as
// reported in the handshake. It is 0 for a connection that has none (an
// in-process one, which has no handshake).
func (c *Conn) HostID() uint64 { return c.Init(wire.HostName) }

// Host is the display itself as a handle: the terminal's theme, the desktop's
// font and status bar, whether there is a desktop at all. One of each exists
// and everyone connected shares it, so what is set here is set for all of them.
func (c *Conn) Host() Handle { return c.Given(wire.HostName) }

// handed is a handle for one of the objects the display hands over, which
// it already knows by name. A connection without a handshake was handed
// neither, and addressing one by a name the display never registered would
// only produce a puzzling refusal, so such a handle keeps to the id it has.
func (c *Conn) handed(id uint64, name string) Handle {
	if id == 0 {
		return Handle{c: c}
	}
	return Handle{c: c, id: id, name: name}
}

// SetApp applies application-wide properties to this connection's app with the
// same syntax as any object: SetApp("multiwindow contextonly") sends
// `set app multiwindow contextonly`. It errors before the handshake has
// assigned an app ID (in-process connections have none).
func (c *Conn) SetApp(props string) (*wire.Reply, error) {
	if c.AppID() == 0 {
		return nil, fmt.Errorf("SetApp: no application id (in-process connection)")
	}
	return c.Exec(fmt.Sprintf("set %s %s", wire.AppName, props))
}

// markClosed fires the Closed channel exactly once.
func (c *Conn) markClosed() { c.closeOnce.Do(func() { close(c.closed) }) }

// Exec executes protocol text on this connection (one batch; the
// remote transport appends the D22 end terminator).
func (c *Conn) Exec(src string) (*wire.Reply, error) {
	return c.transport.Exec(src)
}

// Describe queries the host's wire vocabulary (D24): the supported
// trinket types and, for each, the properties it accepts with each
// property's kind, default, and a brief description. Common properties
// (accepted by every non-virtual type) are reported once.
func (c *Conn) Describe() (*wire.Vocabulary, error) {
	reply, err := c.transport.Exec("describe")
	if err != nil {
		return nil, err
	}
	return wire.DecodeVocabulary(reply.Extra)
}

// Close releases the connection (closes the socket for remote
// connections; no-op in-process).
func (c *Conn) Close() error {
	err := c.transport.Close()
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

// Deliver folds an event into the replica, then invokes handlers. A
// transport calls it for every event that reaches it; filtering by
// subscription and by suppression has already happened upstream of here.
func (c *Conn) Deliver(ev *wire.Event) { c.deliver(ev) }

// Record notes what an object was constructed as. target is the real
// constructed object where a transport has one to offer (in-process) and
// nil where it does not (remote), and reaches an app through Handle.Target.
func (c *Conn) Record(id uint64, typeName string, target any) {
	c.mu.Lock()
	c.types[id] = typeName
	if target != nil {
		c.targets[id] = target
	}
	c.mu.Unlock()
}

// deliver folds an event into the replica, then invokes handlers.
// (BindContext already filtered by subscription and suppression.)
func (c *Conn) deliver(ev *wire.Event) {
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
	var fns []func(*wire.Event)
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
func (c *Conn) on(id uint64, event string, fn func(*wire.Event)) {
	c.ensureSub(id, event)
	c.mu.Lock()
	if c.handlers[id] == nil {
		c.handlers[id] = make(map[string][]func(*wire.Event))
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
	c.typeHandlers["command"] = append(c.typeHandlers["command"], func(ev *wire.Event) {
		if a, ok := ev.Word("action"); ok && a == action {
			fn()
		}
	})
	c.mu.Unlock()
}

// OnType registers a handler for every event of a type that reaches this
// connection, whichever object raised it -- and WITHOUT subscribing to
// anything. It is observation: what arrives is what some subscription
// elsewhere, or the event's own unconditional flow, has already let through.
//
// Handle.On is the way to ask for an object's events. This is the way to watch
// what comes.
func (c *Conn) OnType(eventType string, fn func(*wire.Event)) {
	c.mu.Lock()
	c.typeHandlers[eventType] = append(c.typeHandlers[eventType], fn)
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
// guarantees it will not echo back). target is whatever names it: the id a
// key surfaced, or the name the display already knows it by.
func (c *Conn) set(target, args string) error {
	_, err := c.Exec(fmt.Sprintf("set %s %s", target, args))
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

// The event the display answers its questions with, and the questions it
// answers. Nothing else reads these back, so an app that means to turn one of
// them over asks first.
const (
	HostState = "host_state"

	AskDark    = "dark"
	AskDesktop = "desktop"

	// The display's debug relay: statements put to another connected
	// application, and one event per statement it says back. Off unless the
	// display was started with it open.
	AskRelay   = "relay"
	EventRelay = "relay"
)

// OnHost registers a handler for what the display says about itself and opens
// the flow for it. Subscribing does not ask: see Handle.Ask.
func (c *Conn) OnHost(event string, fn func(*wire.Event)) { c.Host().On(event, fn) }
