package display

// The pane above the list: two policies that apply to every client at once,
// rather than to whichever row is selected.

import (
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
)

// fakeHost is a server as far as the window is concerned: two switches it can
// read and throw, and a word for where each of them came from.
type fakeHost struct {
	trusted bool
	prompt  bool
	origins map[string]string
	kept    string // what a change is said to be kept in; "" = nowhere
}

func (h *fakeHost) PreTrustedOnly() bool { return h.trusted }
func (h *fakeHost) PromptLocal() bool    { return h.prompt }

func (h *fakeHost) SetPolicy(name string, on bool) {
	switch name {
	case PolicyPreTrustedOnly:
		h.trusted = on
	case PolicyPromptLocal:
		h.prompt = on
	default:
		return
	}
	if h.origins == nil {
		h.origins = map[string]string{}
	}
	h.origins[name] = h.kept
}

func (h *fakeHost) PolicyOrigin(name string) string { return h.origins[name] }

// The switches are in the pane above the list, one under the other, each with
// the note that says where its value came from, and worded the way the user
// thinks of them.
func TestTheTwoSwitchesStandAboveTheList(t *testing.T) {
	v := paneWithHost(t, &fakeHost{}, storeWith(t), tempNicknames(t), tempSeen(t))

	first, ok := v.trusted.Parent().(*trinkets.Panel)
	if !ok {
		t.Fatalf("the first switch hangs off a %T", v.trusted.Parent())
	}
	second, ok := v.loopback.Parent().(*trinkets.Panel)
	if !ok {
		t.Fatalf("the second switch hangs off a %T", v.loopback.Parent())
	}
	if first == second {
		t.Fatal("both switches are on one row, so they do not stack")
	}
	if v.trustedFrom.Parent() != first || v.loopbackFrom.Parent() != second {
		t.Error("a switch and the note about it are on different rows")
	}

	above, ok := first.Parent().(*trinkets.Panel)
	if !ok || second.Parent() != above {
		t.Fatal("the two rows are not in one pane")
	}
	if kids := above.Children(); len(kids) != 2 || kids[0] != first || kids[1] != second {
		t.Fatalf("the pane above holds %d rows in another order", len(kids))
	}
	if got := v.trusted.Text(); got != "Allow Previously Trusted Clients Only" {
		t.Errorf("the first switch reads %q", got)
	}
	if got := v.loopback.Text(); got != "Automatically Approve Loopback Connections" {
		t.Errorf("the second switch reads %q", got)
	}
}

// Each switch says where its value came from, so a setting nobody in this
// session chose can still account for itself.
func TestASwitchSaysWhereItsValueCameFrom(t *testing.T) {
	host := &fakeHost{
		origins: map[string]string{
			PolicyPreTrustedOnly: "kittytk.ini",
			PolicyPromptLocal:    PromptLocalEnv,
		},
		kept: "current",
	}
	v := paneWithHost(t, host, storeWith(t), tempNicknames(t), tempSeen(t))

	if got := v.trustedFrom.Text(); got != "(kittytk.ini)" {
		t.Errorf("the lockdown switch says %q, not the file it came from", got)
	}
	if got := v.loopbackFrom.Text(); got != "("+PromptLocalEnv+")" {
		t.Errorf("the loopback switch says %q, not the variable that set it", got)
	}

	// Throwing one moves it to wherever the host says it was kept.
	v.trusted.SetChecked(true)
	if got := v.trustedFrom.Text(); got != "(current)" {
		t.Errorf("after being changed the switch still says %q", got)
	}
}

// And the note is laid out for the words it now holds. It is rewritten when a
// switch is thrown, and a note still arranged for the shorter words before it
// is cut off in the middle.
func TestTheNoteFitsWhatItNowSays(t *testing.T) {
	host := &fakeHost{kept: "a place with a much longer name than any file"}
	v := paneWithHost(t, host, storeWith(t), tempNicknames(t), tempSeen(t))

	for _, step := range []func(){
		func() { v.trusted.SetChecked(true) },
		func() { v.loopback.SetChecked(false) },
	} {
		step()
		for _, note := range []*trinkets.Label{v.trustedFrom, v.loopbackFrom} {
			if b, want := note.Bounds(), note.SizeHint().Width; b.Width < want {
				t.Errorf("%q sits in %d units and needs %d", note.Text(), b.Width, want)
			}
		}
	}
}

// A change nothing keeps says so, rather than naming a place it did not go.
func TestAChangeNobodyKeepsSaysSo(t *testing.T) {
	host := &fakeHost{origins: map[string]string{PolicyPromptLocal: "kittytk.ini"}}
	v := paneWithHost(t, host, storeWith(t), tempNicknames(t), tempSeen(t))

	v.loopback.SetChecked(false)
	if got := v.loopbackFrom.Text(); got != "(this session)" {
		t.Errorf("the switch says %q about a change nothing wrote down", got)
	}
}

// They open showing what the server holds. The loopback switch is the server's
// question turned around -- it asks about local clients, the switch says they
// are waved through -- so an unchecked one must mean the server is asking.
func TestTheSwitchesOpenWhereTheServerStands(t *testing.T) {
	for _, h := range []fakeHost{
		{trusted: false, prompt: false},
		{trusted: true, prompt: true},
		{trusted: true, prompt: false},
	} {
		host := h
		v := paneWithHost(t, &host, storeWith(t), tempNicknames(t), tempSeen(t))
		if v.trusted.IsChecked() != host.trusted {
			t.Errorf("%+v: the lockdown switch reads %v", host, v.trusted.IsChecked())
		}
		if v.loopback.IsChecked() == host.prompt {
			t.Errorf("%+v: the loopback switch reads %v, which says the opposite of "+
				"what the server does", host, v.loopback.IsChecked())
		}
		if !v.trusted.IsEnabled() || !v.loopback.IsEnabled() {
			t.Errorf("%+v: a switch that a server is listening to was disabled", host)
		}
	}
}

// And throwing one changes the server, not just the window.
func TestThrowingASwitchReachesTheServer(t *testing.T) {
	host := &fakeHost{}
	v := paneWithHost(t, host, storeWith(t), tempNicknames(t), tempSeen(t))

	v.trusted.SetChecked(true)
	if !host.trusted {
		t.Error("the server still admits clients it has never been told about")
	}
	v.trusted.SetChecked(false)
	if host.trusted {
		t.Error("the lockdown could be turned on but not off")
	}

	v.loopback.SetChecked(false)
	if !host.prompt {
		t.Error("unchecking the loopback switch did not make the server ask about " +
			"same-machine clients")
	}
	v.loopback.SetChecked(true)
	if host.prompt {
		t.Error("re-checking the loopback switch did not stop the asking")
	}
}

// A window with no server behind it offers switches that cannot be thrown,
// rather than switches that look live and change nothing.
func TestSwitchesWithNoServerAreDead(t *testing.T) {
	v := paneFor(t, storeWith(t), tempNicknames(t), tempSeen(t))
	if v.trusted.IsEnabled() || v.loopback.IsEnabled() {
		t.Error("the switches are live with nothing behind them to change")
	}
}

// The switch the window throws is the one the gate reads: a local client is
// admitted for being local, and once the server is told to ask, the same
// client goes to the authorizer.
func TestTheLoopbackSwitchGovernsTheGate(t *testing.T) {
	asked := 0
	s := &Server{
		store: newAuthStore(filepath.Join(t.TempDir(), "authorizations")),
		authorize: func(AuthRequest) AuthDecision {
			asked++
			return AuthDenyOnce
		},
	}
	local := AuthRequest{AppName: "Editor", Fingerprint: "sha256:aaa", Local: true}

	if !s.admit(local, "") {
		t.Fatal("a same-machine client was refused while loopback is waved through")
	}
	if asked != 0 {
		t.Errorf("the authorizer was asked %d times about a local client", asked)
	}

	s.SetPromptLocal(true)
	if s.admit(local, "") {
		t.Error("the same client was admitted after the server was told to ask")
	}
	if asked != 1 {
		t.Errorf("the authorizer was asked %d times, want once", asked)
	}
}
