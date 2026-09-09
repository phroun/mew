package display

// The pane above the list: two policies that apply to every client at once,
// rather than to whichever row is selected.

import (
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
)

// fakeHost is a server as far as the window is concerned: two switches it can
// read and throw.
type fakeHost struct {
	trusted bool
	prompt  bool
}

func (h *fakeHost) PreTrustedOnly() bool     { return h.trusted }
func (h *fakeHost) SetPreTrustedOnly(v bool) { h.trusted = v }
func (h *fakeHost) PromptLocal() bool        { return h.prompt }
func (h *fakeHost) SetPromptLocal(v bool)    { h.prompt = v }

// The switches are in the pane above the list, one under the other, worded the
// way the user thinks of them.
func TestTheTwoSwitchesStandAboveTheList(t *testing.T) {
	v := paneWithHost(t, &fakeHost{}, storeWith(t), tempNicknames(t), tempSeen(t))

	above, ok := v.trusted.Parent().(*trinkets.Panel)
	if !ok {
		t.Fatalf("the first switch hangs off a %T", v.trusted.Parent())
	}
	if v.loopback.Parent() != above {
		t.Fatal("the two switches are in different panes")
	}
	kids := above.Children()
	if len(kids) != 2 || kids[0] != v.trusted || kids[1] != v.loopback {
		t.Fatalf("the pane above holds %d children in another order", len(kids))
	}
	if got := v.trusted.Text(); got != "Allow Previously Trusted Clients Only" {
		t.Errorf("the first switch reads %q", got)
	}
	if got := v.loopback.Text(); got != "Automatically Approve Loopback Connections" {
		t.Errorf("the second switch reads %q", got)
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
