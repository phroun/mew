package app

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// noFactory is a Factory that never constructs anything: the `set` verb
// operates on an already-registered object, so New is never called.
type noFactory struct{}

func (noFactory) New(string) (protocol.Object, error) {
	return nil, fmt.Errorf("noFactory: new is not supported")
}

// An application registered in a session is addressable by its ObjectID and
// accepts application-wide property sets with the same `set` syntax used for
// windows and trinkets - the wire path a client drives via Conn.SetApp.
func TestApplicationSetViaProtocol(t *testing.T) {
	a := New(nil)
	if a.MultiWindow() || a.ContextOnly() {
		t.Fatalf("preconditions: app should start single-window, not context-only")
	}

	a.SetWireNameChangeAllowed(true) // this connection may rename (see gating test)

	s := protocol.NewSession()
	s.Register(a)

	script, err := protocol.Parse(fmt.Sprintf(`set %d multiwindow contextonly name="Tools"`, a.ObjectID()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.Execute(script, noFactory{}); err != nil {
		t.Fatalf("set via protocol: %v", err)
	}

	if !a.MultiWindow() {
		t.Error("multiwindow flag not applied over the wire")
	}
	if !a.ContextOnly() {
		t.Error("contextonly flag not applied over the wire")
	}
	if a.Name() != "Tools" {
		t.Errorf("name = %q, want Tools", a.Name())
	}

	// An unknown property is a clean error, not a silent no-op.
	bad, _ := protocol.Parse(fmt.Sprintf(`set %d bogus=1`, a.ObjectID()))
	if _, err := s.Execute(bad, noFactory{}); err == nil {
		t.Error("expected an error setting an unknown application property")
	}
}

// The name property is gated: rejected over the wire until the connection is
// authorized to rename, and the app's name stays put on a rejected attempt.
func TestApplicationNameChangeGated(t *testing.T) {
	a := New(nil)
	a.SetName("Original")

	s := protocol.NewSession()
	s.Register(a)
	rename, _ := protocol.Parse(fmt.Sprintf(`set %d name="Renamed"`, a.ObjectID()))

	// A change to a DIFFERENT name is rejected for a name-specific connection.
	if _, err := s.Execute(rename, noFactory{}); err == nil {
		t.Error("name change should be rejected before the connection is authorized")
	}
	if a.Name() != "Original" {
		t.Errorf("name = %q, want Original (rejected change must not apply)", a.Name())
	}

	// Re-asserting the SAME (authorized) name matches the approval and is fine.
	same, _ := protocol.Parse(fmt.Sprintf(`set %d name="Original"`, a.ObjectID()))
	if _, err := s.Execute(same, noFactory{}); err != nil {
		t.Errorf("re-setting the authorized name should be allowed: %v", err)
	}

	// Once the connection is trusted independently of the name, any rename goes.
	a.SetWireNameChangeAllowed(true)
	if _, err := s.Execute(rename, noFactory{}); err != nil {
		t.Fatalf("authorized name change failed: %v", err)
	}
	if a.Name() != "Renamed" {
		t.Errorf("name = %q, want Renamed", a.Name())
	}
}

// Every application gets a stable, unique, non-zero protocol identity on
// creation - the same ObjectID space windows and trinkets draw from - so a
// running app can be referred to (and, in time, set) over the protocol.
func TestApplicationObjectID(t *testing.T) {
	a := New(nil)
	b := New(nil)
	s := NewSecondary()

	if a.ObjectID() == 0 || b.ObjectID() == 0 || s.ObjectID() == 0 {
		t.Fatalf("object IDs must be non-zero: %d %d %d", a.ObjectID(), b.ObjectID(), s.ObjectID())
	}
	if a.ObjectID() == b.ObjectID() || a.ObjectID() == s.ObjectID() || b.ObjectID() == s.ObjectID() {
		t.Errorf("object IDs must be unique: %d %d %d", a.ObjectID(), b.ObjectID(), s.ObjectID())
	}
	if a.ObjectID() != a.ObjectID() {
		t.Error("ObjectID must be stable across calls")
	}
}
