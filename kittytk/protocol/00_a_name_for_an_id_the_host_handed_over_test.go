package protocol

// Some objects a client holds were never built by it: the application and the
// store arrive in the handshake as bare numbers, so everything said to them
// had to be said by number. `name=<id>` is the other half of the surfacing
// form -- the same statement that names a key path names an id -- and after it
// the two kinds of object read alike.
//
// The id is checked against the session's own table, which is the whole of the
// gate: ids come from one counter across every connection, so another client's
// are easy to guess and reach nothing.

import (
	"strings"
	"testing"
)

// handed is an object the host registers into a session without the wire
// having built it -- an application or a store, as far as this test cares.
type handed struct {
	id    uint64
	name  string
	calls int
}

func (h *handed) ID() uint64 { return h.id }

func (h *handed) Set(name string, v *Value, f FlagState) error {
	s, err := AsString("name", v, f)
	if err != nil {
		return err
	}
	h.name, h.calls = s, h.calls+1
	return nil
}

func (h *handed) Append(string, Object) error { return nil }

// hostSession is a session holding one object the host registered, the way a
// connection holds its application.
func hostSession(id uint64) (*Session, *handed, Factory) {
	obj := &handed{id: id}
	s := NewSession()
	s.Register(obj)
	return s, obj, NewRegistryFactory(&BindContext{})
}

// run executes src and returns whatever the session made of it.
func run(t *testing.T, s *Session, f Factory, src string) (*Reply, error) {
	t.Helper()
	script, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return s.Execute(script, f)
}

func TestAnIDTheHostHandedOverCanBeGivenAName(t *testing.T) {
	s, obj, f := hostSession(6)

	reply, err := run(t, s, f, "app=6\nset app name=\"Tools\"")
	if err != nil {
		t.Fatalf("naming and then setting: %v", err)
	}
	if obj.name != "Tools" || obj.calls != 1 {
		t.Errorf("the object was set to %q %d times, want Tools once", obj.name, obj.calls)
	}
	// The name comes back like any other surfaced one, so a client that lost
	// track of the number can read it off the reply.
	if reply.IDs["app"] != 6 {
		t.Errorf("reply surfaced app=%d, want 6", reply.IDs["app"])
	}
}

// The name is a session key like any other, so every verb takes it -- there is
// no second kind of target.
func TestTheNameWorksForEveryVerbThatTakesATarget(t *testing.T) {
	s, _, f := hostSession(6)
	if _, err := run(t, s, f, "app=6"); err != nil {
		t.Fatalf("naming: %v", err)
	}
	for _, src := range []string{
		`set app name="x"`,
		`ask app anything`,
		`sub app`,
	} {
		_, err := run(t, s, f, src)
		if err != nil && strings.Contains(err.Error(), "unknown key path") {
			t.Errorf("%q did not recognise the name: %v", src, err)
		}
	}
}

// An id this session does not hold is refused, which is what stops the form
// from being a way to reach across connections.
func TestAnIDThisSessionDoesNotHoldIsRefused(t *testing.T) {
	s, _, f := hostSession(6)

	// 12 is another connection's application: a real object, in someone
	// else's session.
	other := NewSession()
	other.Register(&handed{id: 12})

	_, err := run(t, s, f, "app=12")
	if err == nil {
		t.Fatal("naming another session's object was allowed")
	}
	if !strings.Contains(err.Error(), "no object with id 12 in this session") {
		t.Errorf("refusal reads %q", err)
	}
	// An id that exists nowhere is refused the same way, so the refusal
	// says nothing about what other connections hold.
	_, missing := run(t, s, f, "app=13")
	if missing == nil || !strings.Contains(missing.Error(), "no object with id 13 in this session") {
		t.Errorf("an id that exists nowhere reads %q", missing)
	}
	if _, ok := s.keys["app"]; ok {
		t.Error("the refused name was registered anyway")
	}
}

// The path form is untouched: it still resolves through the key table.
func TestTheNameStillReachesAKeyPath(t *testing.T) {
	s, _, f := hostSession(6)
	if _, err := run(t, s, f, "it=new testplain\nsame=it"); err != nil {
		t.Fatalf("surfacing a path: %v", err)
	}
	if s.keys["same"] == 0 || s.keys["same"] != s.keys["it"] {
		t.Errorf("same=%d, it=%d", s.keys["same"], s.keys["it"])
	}
	if _, err := run(t, s, f, "gone=nowhere"); err == nil ||
		!strings.Contains(err.Error(), `unknown key path "nowhere"`) {
		t.Errorf("an unknown path reads %v", err)
	}
}

// An id is not a verb, so nothing follows it. Saying otherwise is a syntax
// error rather than a statement with a silently dropped tail.
func TestNothingFollowsAnID(t *testing.T) {
	s, _, f := hostSession(6)
	_, err := run(t, s, f, `app=6 name="Tools"`)
	if err == nil {
		t.Fatal("arguments after an id were accepted")
	}
	if !strings.Contains(err.Error(), "not a command") {
		t.Errorf("the refusal reads %q", err)
	}
}

// Ids are whole and positive. The rest of the number syntax the language has
// is not a way to name something.
func TestOnlyAWholeIDNamesAnything(t *testing.T) {
	s, _, f := hostSession(6)
	for _, src := range []string{"app=6.5", "app=-6"} {
		if _, err := run(t, s, f, src); err == nil {
			t.Errorf("%q was accepted", src)
		}
	}
	// Zero parses, and then finds nothing: it is no object's id.
	if _, err := run(t, s, f, "app=0"); err == nil ||
		!strings.Contains(err.Error(), "no object with id 0") {
		t.Errorf("app=0 reads %v", err)
	}
}
