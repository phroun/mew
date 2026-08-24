package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// runScript executes wire text against a real registry factory and returns
// whatever the session said about it.
func runScript(t *testing.T, src string) error {
	t.Helper()
	ctx := &protocol.BindContext{Emit: func(*protocol.Event) {}}
	f := protocol.NewRegistryFactory(ctx)
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	_, err = protocol.NewSession().Execute(script, f)
	return err
}

// A subscription names an event the target actually raises, or it is refused.
//
// It used to be accepted whatever was written. A client that misspelled an
// event got no complaint and then waited forever for something nothing was
// ever going to raise -- the failure told it nothing was wrong. The registry
// already knows what each type emits, being the same table `describe` reports
// from, so the answer is available at the moment the subscription is made.
func TestSubRefusesAnEventTheTypeDoesNotRaise(t *testing.T) {
	if err := runScript(t, `b=new button caption="ok"
sub b clik`); err == nil {
		t.Fatal("sub accepted a misspelled event name")
	} else {
		msg := err.Error()
		// The complaint has to be usable: which type, which name, and what it
		// could have been instead.
		for _, want := range []string{"button", "clik", "click"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q does not mention %q", msg, want)
			}
		}
	}
}

// The name it does raise goes through.
func TestSubAcceptsAnEventTheTypeRaises(t *testing.T) {
	if err := runScript(t, `b=new button caption="ok"
sub b click`); err != nil {
		t.Errorf("sub button click: %v", err)
	}
}

// Unsubscribing is checked the same way. An unsub nothing could have
// subscribed to is the same typo, and silently doing nothing about it leaves
// the client believing it has stopped a flow it has not.
func TestUnsubIsCheckedToo(t *testing.T) {
	if err := runScript(t, `b=new button caption="ok"
sub b click
unsub b clik`); err == nil {
		t.Error("unsub accepted a misspelled event name")
	}
}

// An event that is real but belongs to another type is refused as well --
// that is the mistake a name-only check would miss.
func TestSubRefusesAnotherTypesEvent(t *testing.T) {
	if err := runScript(t, `b=new button caption="ok"
sub b toggle`); err == nil {
		t.Error("a button accepted the checkbox's toggle event")
	}
	if err := runScript(t, `c=new checkbox caption="x"
sub c toggle`); err != nil {
		t.Errorf("a checkbox refused its own toggle event: %v", err)
	}
}

// `sub all` names every trinket there is, so there is no one type to check
// against and the question widens to whether ANYTHING raises the event. That
// still catches the typo without inventing a rule about which trinket was
// meant.
func TestSubAllChecksAgainstEveryType(t *testing.T) {
	if err := runScript(t, `sub all click`); err != nil {
		t.Errorf("sub all click: %v", err)
	}
	if err := runScript(t, `sub all clik`); err == nil {
		t.Error("sub all accepted an event no type raises")
	}
}

// Subscribing with no event names at all still means "everything this target
// raises", and has nothing to check.
func TestSubWithNoEventNamesIsUnchecked(t *testing.T) {
	if err := runScript(t, `b=new button caption="ok"
sub b`); err != nil {
		t.Errorf("bare sub: %v", err)
	}
	if err := runScript(t, `sub all`); err != nil {
		t.Errorf("sub all: %v", err)
	}
}

// command flows unconditionally, so it is subscribable on anything -- including
// a type whose own table does not list it.
func TestCommandIsAlwaysSubscribable(t *testing.T) {
	for _, src := range []string{
		`b=new button caption="ok"` + "\n" + `sub b command`,
		`p=new panel` + "\n" + `sub p command`,
		`sub all command`,
	} {
		if err := runScript(t, src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}
