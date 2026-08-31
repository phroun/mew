// Package inprocess connects a client to the trinket vocabulary
// registered in this same process, with no socket between them.
//
// It is host code: it reaches for the registry and the session, and so
// carries the toolkit with it. That is why it is not part of package
// client -- everything there needs the wire language alone, and an
// application that speaks the protocol over a socket should not have to
// compile the rendering side to do it. A host embedding the toolkit uses
// this; an ordinary application does not, and should not.
package inprocess

import (
	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
)

// New creates a connection whose display side is the registered trinket
// vocabulary in this process. dispatch receives action= command IDs
// (pass the application registry's Dispatch; nil is allowed for
// connections that use no commands).
func New(dispatch func(commandID string)) *client.Conn {
	t := &transport{session: protocol.NewSession()}
	// Commands arrive uniformly as command events (Deliver invokes the
	// dispatch sink), so the BindContext dispatch stays nil - FireAction
	// still emits the event, and there is exactly one dispatch path
	// in-process and remote alike.
	conn := client.NewWithTransport(t, dispatch)
	ctx := &protocol.BindContext{Emit: conn.Deliver}
	t.factory = &recordingFactory{conn: conn, inner: protocol.NewRegistryFactory(ctx)}
	return conn
}

// transport executes against the local session and factory.
type transport struct {
	session *protocol.Session
	factory protocol.Factory
}

func (t *transport) Exec(src string) (*protocol.Reply, error) {
	script, err := protocol.Parse(src)
	if err != nil {
		return nil, err
	}
	return t.session.Execute(script, t.factory)
}

func (t *transport) Close() error { return nil }

// recordingFactory interposes on construction to report each object's
// type and its real constructed target to the connection's replica.
type recordingFactory struct {
	conn  *client.Conn
	inner protocol.Factory
}

func (f *recordingFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	var target any
	if tg, ok := o.(interface{ Target() any }); ok {
		target = tg.Target()
	}
	f.conn.Record(o.ID(), typeName, target)
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
