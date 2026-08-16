package input

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// keyNames pumps the handler and returns the next n key names.
func keyNames(t *testing.T, kh *KeyboardHandler, n int) []string {
	t.Helper()
	evs := make(chan InputEvent, 8)
	go func() {
		for {
			evs <- kh.GetEvent()
		}
	}()
	var out []string
	for len(out) < n {
		select {
		case ev := <-evs:
			if ev.Key != "" {
				out = append(out, ev.Key)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d of %d keys: %v", len(out), n, out)
		}
	}
	return out
}

// mew has to ASK for key releases, and never did.
//
// A key coming back up has no legacy encoding — there are no bytes for it — so
// a terminal sends one only when an application has pushed the kitty keyboard
// protocol's event-reporting flag. mew pushed nothing, so nothing arrived, and
// a child in one of mew's terminal panes could not be given what mew itself was
// never sent. That is the whole reason a browser hosted in a mew terminal saw
// keydown without keyup.
//
// The pop matters as much as the push: flags left standing outlive the process
// and land on the shell mew hands the terminal back to.
func TestSessionAsksForKeyEventsAndGivesThemBack(t *testing.T) {
	var termOut bytes.Buffer
	kh := NewKeyboardHandler(strings.NewReader(""), &termOut)
	if err := kh.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := termOut.String(); !strings.Contains(got, "\x1b[>2u") {
		t.Errorf("startup wrote %q, with no push of the event-reporting flag; "+
			"without it the terminal sends no key release at all", got)
	}
	termOut.Reset()

	kh.Stop()
	if got := termOut.String(); !strings.Contains(got, "\x1b[<u") {
		t.Errorf("shutdown wrote %q, never popping the flags it pushed; "+
			"the shell inherits them", got)
	}
}

// Both markers survive the trip from wire to key name.
//
// Event reporting buys them together: a held key starts reporting ":Repeat"
// instead of another plain press. Neither is folded away here — mew's keymap
// cannot read either one, but the child in a terminal pane can read both, and
// the layer that cannot represent a thing is not the layer that gets to discard
// it. Where each marker is set aside is Editor.dispatchKey's business.
func TestReleaseAndRepeatArriveMarked(t *testing.T) {
	// Release (:3) then repeat (:2), in both of the shapes the protocol uses:
	// the "u" form for a text key and the cursor-key form for an arrow. The
	// arrow is here because its family was the one that silently reported a
	// release as another PRESS, so a held arrow key moved the cursor twice.
	input := strings.NewReader("\x1b[97;1:3u\x1b[97;1:2u\x1b[1;1:3A\x1b[1;1:2A")
	kh := NewKeyboardHandler(input, &bytes.Buffer{})
	if err := kh.handler.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kh.handler.Stop()

	got := keyNames(t, kh, 4)
	want := []string{"a:Release", "a:Repeat", "up:Release", "up:Repeat"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d = %q, want %q (got %v)", i, got[i], want[i], got)
		}
	}
}
