package display

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
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

// Typing a nickname over a client's row writes it, which is the whole of what
// the first column is for. Driven the way a user drives it -- the keys that
// open the editor, type into it, and commit -- because every part of that path
// is somewhere the name can be dropped.
func TestRenamingAClientInTheWindowSavesIt(t *testing.T) {
	nicks := tempNicknames(t)
	v := paneFor(t, storeWith(t, "allow client sha256:aaa"), nicks, tempKnown(t))
	selectRow(t, v, 1, "")

	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	if !v.tree.RowEditing() {
		t.Fatal("the editor did not open on a client's row")
	}
	for _, r := range "laptop" {
		v.tree.HandleKeyPress(core.KeyPressEvent{Key: string(r), Text: string(r)})
	}
	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	if v.tree.RowEditing() {
		t.Fatal("the editor did not close, so nothing was committed")
	}

	if got := nicks.get("sha256:aaa"); got != "laptop" {
		t.Errorf("the store holds %q after the client was renamed to laptop", got)
	}
	if got := v.tree.RootItems()[1].Text; got != "laptop" {
		t.Errorf("the row reads %q", got)
	}
	// And it is still there for the next window, which is what saving means.
	rows := connectionsRows(v.store, nicks.all(), nil)
	if rows[1].name != "laptop" {
		t.Errorf("a window opened again shows %q", rows[1].name)
	}
}

// Clearing the name puts the row back to saying it has none, rather than
// leaving an empty first column.
func TestClearingANicknameInTheWindowSaysUnnamed(t *testing.T) {
	nicks := tempNicknames(t)
	if err := nicks.set("sha256:aaa", "laptop"); err != nil {
		t.Fatal(err)
	}
	v := paneFor(t, storeWith(t, "allow client sha256:aaa"), nicks, tempKnown(t))
	selectRow(t, v, 1, "")

	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	if !v.tree.RowEditing() {
		t.Fatal("the editor did not open")
	}
	// The editor opens with the name selected: a letter replaces the whole of
	// it, and rubbing that out leaves nothing.
	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "x", Text: "x"})
	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "Backspace"})
	v.tree.HandleKeyPress(core.KeyPressEvent{Key: "Return"})

	if got := nicks.get("sha256:aaa"); got != "" {
		t.Errorf("the store still holds %q", got)
	}
	if got := v.tree.RootItems()[1].Text; got != "(unnamed)" {
		t.Errorf("the row reads %q, not that it has no name", got)
	}
}
