package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// menuCapture wraps a factory to grab the first MenuItem it builds, so a
// test can trigger it directly.
type menuCapture struct {
	inner protocol.Factory
	item  *MenuItem
}

func (f *menuCapture) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	if b, ok := o.(interface{ Target() any }); ok {
		if mi, ok := b.Target().(*MenuItem); ok && f.item == nil {
			f.item = mi
		}
	}
	return o, nil
}

func (f *menuCapture) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *menuCapture) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *menuCapture) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}

const menuActionScript = `bar=new menubar children={new menu caption="File" children={new menuitem caption="New" action=demo.act}}`

// Over a connection that consumes events (the display protocol),
// triggering a protocol-built menu item must emit a command event -
// the same seam buttons use - so a remote app receives its menu actions.
func TestMenuItemActionEmitsCommandOverWire(t *testing.T) {
	var events []*protocol.Event
	ctx := &protocol.BindContext{Emit: func(ev *protocol.Event) { events = append(events, ev) }}
	f := &menuCapture{inner: protocol.NewRegistryFactory(ctx)}

	script, err := protocol.Parse(menuActionScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatal(err)
	}
	if f.item == nil {
		t.Fatal("no menuitem captured")
	}
	if f.item.OnTriggered == nil {
		t.Fatal("menu item has no trigger handler with an event sink present")
	}

	f.item.Trigger()

	var found bool
	for _, ev := range events {
		if ev.Type == "command" {
			if a, ok := ev.Word("action"); ok && a == "demo.act" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("Trigger emitted no command action=demo.act (events=%v)", events)
	}
}

// In-process menus build with no event sink and dispatch through a real
// command registry; the protocol Bind must NOT install a trigger handler
// there, or it would shadow the app's registered command handlers.
func TestMenuItemInProcessLeavesRegistryPath(t *testing.T) {
	f := &menuCapture{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
	script, err := protocol.Parse(menuActionScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatal(err)
	}
	if f.item == nil {
		t.Fatal("no menuitem captured")
	}
	if f.item.OnTriggered != nil {
		t.Error("menu item installed a trigger handler without an event sink; app registry would be shadowed")
	}
}
