package client

// The in-process transport: a Conn whose display side is the trinket
// vocabulary registered in this same process, with no socket between
// them. A host embedding the toolkit uses it; an ordinary application
// does not, and should not.
//
// It is the only part of this package that reaches for the registry
// and the session, which is why it sits in its own file: everything
// else here needs the wire language alone. Moving this out of the
// package is what would let the client ship separately from the
// toolkit -- it needs an exported transport seam first, since it
// builds on unexported Conn internals.

import "github.com/phroun/kittytk/protocol"

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
