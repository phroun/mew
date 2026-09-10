package display_test

// The terminal's theme, the desktop's font and status bar, whether the desktop
// is showing: one of each, shared by everyone connected. They were bare verbs
// with no object to belong to; they are properties of `host` now.

import (
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

func TestTheDisplaysOwnPropertiesAreSetOnTheHost(t *testing.T) {
	conn := dialDesktop(t, "Host App")

	for _, prop := range []string{
		"dark",
		"!dark",
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
	// The two that are the app's own are untouched.
	for _, verb := range []string{"announce_visual", "announce_speak"} {
		if _, err := conn.Exec(verb); err != nil {
			t.Errorf("%q: %v", verb, err)
		}
	}
}

// What the display DOES holds no value afterwards, so it is neither a property
// to set nor a question to ask.
func TestWhatTheDisplayDoesIsDoneNotSet(t *testing.T) {
	conn := dialDesktop(t, "Doing App")

	for _, action := range []string{
		"tile", "cascade", "rawkey", "cut", "copy", "paste", "selectall",
	} {
		if err := conn.Host().Do(action); err != nil {
			t.Errorf("do host %s: %v", action, err)
		}
		// Not a property: the same word set is refused.
		if err := conn.Host().Set(action); err == nil {
			t.Errorf("set host %s was accepted", action)
		}
	}

	// And an action the display has not got is refused rather than dropped.
	if err := conn.Host().Do("nonsense"); err == nil {
		t.Error("do host nonsense was accepted")
	}
	// The bare verbs they were are gone with them.
	for _, verb := range []string{"tile", "cascade", "rawkey", "copy"} {
		if _, err := conn.Exec(verb); err == nil {
			t.Errorf("%q is still a verb", verb)
		}
	}
}

// The two flags mean what a flag means. Both directions are the request; a
// third state is not one of the two, and neither is a value.
func TestTheFlagsTakeTwoStatesAndNoOther(t *testing.T) {
	conn := dialDesktop(t, "Asserting App")

	for _, prop := range []string{"?dark", "dark=on", "?desktop", "desktop=on"} {
		if err := conn.Host().Set(prop); err == nil {
			t.Errorf("set host %s was accepted", prop)
		}
	}
	// `dark=true` and `dark=false` are the word spellings AsBool accepts, so
	// they are not refusals -- the point is that a third state is.
	for _, prop := range []string{"dark=true", "dark=false"} {
		if err := conn.Host().Set(prop); err != nil {
			t.Errorf("set host %s: %v", prop, err)
		}
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
	if _, err := conn.Exec("set host dark"); err != nil {
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
	if _, err := conn.Exec("set host dark"); err != nil {
		t.Errorf("set host dark: %v", err)
	}
	// And numbered, for a client that took the name for something else.
	if _, err := conn.Exec(`host=new window title="Mine" width=80 height=60`); err != nil {
		t.Fatalf("taking the name: %v", err)
	}
	if err := (client.Handle{}).Valid(); err {
		t.Fatal("an empty handle reported itself valid")
	}
	if _, err := conn.Exec("set " + itoa(conn.HostID()) + " dark"); err != nil {
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

// Nothing else reads these back, so an app that means to turn one of them over
// has to be told which way it is first.
func TestTheDisplaySaysHowItStands(t *testing.T) {
	conn := dialDesktop(t, "Asking App")

	said := make(chan *wire.Event, 8)
	conn.OnHost(client.HostState, func(ev *wire.Event) { said <- ev })

	answer := func(t *testing.T, what string) *wire.Event {
		t.Helper()
		select {
		case ev := <-said:
			return ev
		case <-time.After(5 * time.Second):
			t.Fatalf("%s was never answered", what)
			return nil
		}
	}

	// Every question is answered with the whole of it, and the answer names
	// the display so one subscription hears it.
	if err := conn.Host().Ask(client.AskDark); err != nil {
		t.Fatalf("ask host dark: %v", err)
	}
	ev := answer(t, "ask host dark")
	if id, _ := ev.Uint("host"); id != conn.HostID() {
		t.Errorf("the answer came from %d, want %d", id, conn.HostID())
	}
	if ev.Flag("dark") == wire.FlagNone || ev.Flag("desktop") == wire.FlagNone {
		t.Errorf("the answer said nothing: %s", ev.Encode())
	}

	// What it says is what was set.
	for _, c := range []struct {
		set  string
		want wire.FlagState
	}{
		{"!dark", wire.FlagFalse},
		{"dark", wire.FlagTrue},
	} {
		if err := conn.Host().Set(c.set); err != nil {
			t.Fatalf("set host %s: %v", c.set, err)
		}
		if err := conn.Host().Ask(client.AskDark); err != nil {
			t.Fatalf("ask: %v", err)
		}
		if got := answer(t, "ask host dark").Flag("dark"); got != c.want {
			t.Errorf("after set host %s the answer says dark=%v, want %v", c.set, got, c.want)
		}
	}

	// Asking about the desktop is answered the same way.
	if err := conn.Host().Ask(client.AskDesktop); err != nil {
		t.Fatalf("ask host desktop: %v", err)
	}
	if answer(t, "ask host desktop").Flag("desktop") == wire.FlagNone {
		t.Error("the answer said nothing about the desktop")
	}

	// And a question it does not answer is refused rather than ignored.
	if _, err := conn.Exec("ask host nonsense"); err == nil {
		t.Error("ask host nonsense was accepted")
	}
}

// A type declares what it does beside what it holds, so `describe` reports it
// and a client learns the actions without being taught them. That is the whole
// reason `do` is one verb rather than a word per action: the language cannot
// grow a verb a type declares, but it can grow an action.
func TestDescribeReportsWhatEachTypeDoes(t *testing.T) {
	conn := dialDesktop(t, "Curious App")

	v, err := conn.Describe()
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	want := map[string][]string{
		"host":    {"cascade", "copy", "cut", "paste", "rawkey", "selectall", "tile"},
		"mdipane": {"cascade", "minimize", "next", "prior", "remove", "restore", "tile"},
		"blob":    {"append"},
	}
	seen := map[string]bool{}
	for _, ty := range v.Types {
		expect, ok := want[ty.Name]
		if !ok {
			continue
		}
		seen[ty.Name] = true
		var got []string
		for _, d := range ty.Does {
			got = append(got, d.Name)
		}
		if strings.Join(got, ",") != strings.Join(expect, ",") {
			t.Errorf("%s does %v, want %v", ty.Name, got, expect)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("describe never mentioned %s", name)
		}
	}

	// The arguments come with them, so a client knows what to send.
	for _, ty := range v.Types {
		if ty.Name != "mdipane" {
			continue
		}
		for _, d := range ty.Does {
			switch d.Name {
			case "restore", "minimize", "remove":
				if len(d.Args) != 1 || d.Args[0].Name != "window" {
					t.Errorf("mdipane %s takes %v, want one window=", d.Name, d.Args)
				}
			default:
				if len(d.Args) != 0 {
					t.Errorf("mdipane %s takes %v, want nothing", d.Name, d.Args)
				}
			}
		}
	}
}
