package display

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempNicknames(t *testing.T) *pairStore {
	t.Helper()
	return newNicknameStore(filepath.Join(t.TempDir(), "nicknames"))
}

// A nickname is written, read back, and replaced rather than accumulated --
// which is what separates it from the authorizations store, where every
// decision is appended and the reader lets deny win.
func TestANicknameIsReplacedNotAccumulated(t *testing.T) {
	s := tempNicknames(t)

	if got := s.get("sha256:aaa"); got != "" {
		t.Errorf("an unnamed client came back as %q", got)
	}
	if err := s.set("sha256:aaa", "the laptop"); err != nil {
		t.Fatal(err)
	}
	if got := s.get("sha256:aaa"); got != "the laptop" {
		t.Errorf("read back %q", got)
	}

	if err := s.set("sha256:aaa", "the other laptop"); err != nil {
		t.Fatal(err)
	}
	if got := s.get("sha256:aaa"); got != "the other laptop" {
		t.Errorf("after renaming, read back %q", got)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "sha256:aaa"); n != 1 {
		t.Errorf("the identity appears %d times in the file; a rename should "+
			"replace the line, not add one:\n%s", n, raw)
	}
}

// A name may hold spaces, since it is what a person calls a machine and people
// do not name things in one word.
func TestANicknameKeepsItsSpaces(t *testing.T) {
	s := tempNicknames(t)
	if err := s.set("ip:10.0.0.4", "the machine under the desk"); err != nil {
		t.Fatal(err)
	}
	if got := s.get("ip:10.0.0.4"); got != "the machine under the desk" {
		t.Errorf("read back %q", got)
	}
}

// Clearing a name removes it rather than storing an empty one, so the window
// falls back to saying the client is unnamed.
func TestClearingANameRemovesIt(t *testing.T) {
	s := tempNicknames(t)
	if err := s.set("sha256:aaa", "temporary"); err != nil {
		t.Fatal(err)
	}
	if err := s.set("sha256:aaa", "   "); err != nil {
		t.Fatal(err)
	}
	if got := s.get("sha256:aaa"); got != "" {
		t.Errorf("a cleared name came back as %q", got)
	}
	if _, ok := s.all()["sha256:aaa"]; ok {
		t.Error("the identity is still in the store with an empty name")
	}
}

// Nothing depends on a nickname: it is written to its own file, so losing it
// cannot change who may connect.
func TestNamingIsSeparateFromDeciding(t *testing.T) {
	auth := storeWith(t, "allow app sha256:aaa Editor")
	nicks := tempNicknames(t)
	if err := nicks.set("sha256:aaa", "the laptop"); err != nil {
		t.Fatal(err)
	}
	if nicks.path == auth.path {
		t.Fatal("the two stores share a file, so a rename rewrites decisions")
	}

	before := auth.entries()
	if err := os.Remove(nicks.path); err != nil {
		t.Fatal(err)
	}
	after := auth.entries()
	if len(before) != len(after) || len(after) != 1 || !after[0].apps[0].allowed() {
		t.Errorf("losing the names changed what was decided: %+v -> %+v",
			before, after)
	}
}

// The prompt offers the name it already has, so a returning client is
// recognised rather than asked about from nothing.
func TestThePromptOffersTheNameAlreadyGiven(t *testing.T) {
	req := AuthRequest{AppName: "Editor", Fingerprint: "sha256:aaa", Transport: "tls"}
	script := authPromptScript(req, "the laptop")

	if !strings.Contains(script, "new textinput") {
		t.Fatal("the prompt has nowhere to write a name")
	}
	if !strings.Contains(script, `text="the laptop"`) {
		t.Errorf("the prompt did not offer the name already given:\n%s", script)
	}
	if !strings.Contains(script, "nickfield=w.root.nrow.nick") {
		t.Error("the field is not surfaced, so nothing can read what was typed")
	}
}
