package display_test

// The terminal's theme, the desktop's font and status bar, whether the desktop
// is showing: one of each, shared by everyone connected. They were bare verbs
// with no object to belong to; they are properties of `host` now.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/client"
)

func TestTheDisplaysOwnPropertiesAreSetOnTheHost(t *testing.T) {
	conn := dialDesktop(t, "Host App")

	for _, prop := range []string{
		"theme",
		"desktop",
		"!desktop",
		"desktopfont=tuesday",
		"desktopfont=default",
		`status="Ready"`,
	} {
		if err := conn.Host().Set(prop); err != nil {
			t.Errorf("set host %s: %v", prop, err)
		}
	}
}

// The words they used to be are gone: nothing answers to them any more, and a
// client that sends one is told so rather than quietly ignored.
func TestTheVerbsTheyWereAreGone(t *testing.T) {
	conn := dialDesktop(t, "Old App")

	for _, verb := range []string{
		"theme", "spawndesktop", "gosolo", "desktopfont tuesday",
		`status text="still here"`,
	} {
		_, err := conn.Exec(verb)
		if err == nil {
			t.Errorf("%q is still a verb", verb)
			continue
		}
		if !strings.Contains(err.Error(), "unknown verb") {
			t.Errorf("%q reads %v", verb, err)
		}
	}
	// The ones with no object of their own are untouched.
	for _, verb := range []string{"tile", "cascade", "rawkey", "copy"} {
		if _, err := conn.Exec(verb); err != nil {
			t.Errorf("%q: %v", verb, err)
		}
	}
}

// Theme is an action: asserting it is the whole of the request, so there is
// nothing to negate and saying otherwise is refused rather than ignored.
func TestAnActionIsAssertedAndNothingElse(t *testing.T) {
	conn := dialDesktop(t, "Asserting App")

	for _, prop := range []string{"!theme", "theme=dark", "?theme"} {
		if err := conn.Host().Set(prop); err == nil {
			t.Errorf("set host %s was accepted", prop)
		}
	}
	// desktop is a state, and takes both directions -- but not a value.
	if err := conn.Host().Set("desktop=on"); err == nil {
		t.Error("set host desktop=on was accepted")
	}
}

// The display is not something a client makes another of.
func TestTheHostIsNotBuiltOverTheWire(t *testing.T) {
	conn := dialDesktop(t, "Building App")

	_, err := conn.Exec(`h=new host`)
	if err == nil {
		t.Fatal("new host was allowed")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("the refusal reads %q", err)
	}
	// And the connection survives being told no.
	if _, err := conn.Exec("set host theme"); err != nil {
		t.Errorf("the connection did not survive: %v", err)
	}
}

// The host arrives named and numbered, like the app and the store.
func TestTheHostArrivesWithTheOthers(t *testing.T) {
	conn := dialDesktop(t, "Arriving App")

	if conn.HostID() == 0 {
		t.Fatal("the handshake carried no host id")
	}
	for _, other := range []uint64{conn.AppID(), conn.StoreID()} {
		if conn.HostID() == other {
			t.Errorf("the host shares id %d with something else", other)
		}
	}
	if got := conn.Host().ID(); got != conn.HostID() {
		t.Errorf("the host handle carries %d, want %d", got, conn.HostID())
	}
	// Named, so nothing has to be written down first.
	if _, err := conn.Exec("set host theme"); err != nil {
		t.Errorf("set host theme: %v", err)
	}
	// And numbered, for a client that took the name for something else.
	if _, err := conn.Exec(`host=new window title="Mine" width=80 height=60`); err != nil {
		t.Fatalf("taking the name: %v", err)
	}
	if err := (client.Handle{}).Valid(); err {
		t.Fatal("an empty handle reported itself valid")
	}
	if _, err := conn.Exec("set " + itoa(conn.HostID()) + " theme"); err != nil {
		t.Errorf("the display no longer answers to its id: %v", err)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
