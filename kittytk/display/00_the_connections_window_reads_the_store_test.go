package display

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeWith writes an authorizations file and returns a store over it.
func storeWith(t *testing.T, lines ...string) *authStore {
	t.Helper()
	p := filepath.Join(t.TempDir(), "authorizations")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return newAuthStore(p)
}

// The window shows what the store decided, gathered per client instead of
// answered for one request: a row for each identity, the apps it names beneath
// it, and the same deny-beats-allow rule the gate itself applies.
func TestTheStoreReadsBackAsRows(t *testing.T) {
	s := storeWith(t,
		"allow app sha256:aaa Editor",
		"allow app sha256:aaa Mailer",
		"allow client sha256:bbb",
		"# a comment, and a blank line follow",
		"",
		"deny app sha256:aaa Scratch",
		"deny client sha256:ccc",
	)

	got := s.entries()
	if len(got) != 3 {
		t.Fatalf("read %d clients, want 3: %+v", len(got), got)
	}
	// First-seen order, which is the order they were approved in.
	if got[0].identity != "sha256:aaa" || got[1].identity != "sha256:bbb" ||
		got[2].identity != "sha256:ccc" {
		t.Errorf("clients came back in the wrong order: %+v", got)
	}
	if !got[1].allow || got[1].deny {
		t.Errorf("a client-wide allow did not read as one: %+v", got[1])
	}
	if !got[2].deny {
		t.Errorf("a client-wide deny did not read as one: %+v", got[2])
	}
	if n := len(got[0].apps); n != 3 {
		t.Fatalf("the first client named %d apps, want 3: %+v", n, got[0].apps)
	}
	for _, a := range got[0].apps {
		want := a.name != "Scratch"
		if a.allowed() != want {
			t.Errorf("%q reads as allowed=%v, want %v", a.name, a.allowed(), want)
		}
	}
}

// Deny beats allow wherever the two lines happen to sit, which is what the gate
// does -- so a client re-approved after being blocked stays blocked until the
// block is removed rather than because of where the line landed.
func TestDenyWinsWhicheverLineCameFirst(t *testing.T) {
	for _, order := range [][]string{
		{"deny app sha256:aaa Editor", "allow app sha256:aaa Editor"},
		{"allow app sha256:aaa Editor", "deny app sha256:aaa Editor"},
	} {
		e := storeWith(t, order...).entries()
		if len(e) != 1 || len(e[0].apps) != 1 {
			t.Fatalf("%v: read back as %+v", order, e)
		}
		if e[0].apps[0].allowed() {
			t.Errorf("%v: a denied app read as allowed", order)
		}
	}
}

// The rows the window draws: this host first, then each client under whatever
// name the user gave it, with its apps beneath.
func TestTheRowsNameThisHostFirst(t *testing.T) {
	s := storeWith(t, "allow app sha256:aaa Editor")
	rows := connectionsRows(s, map[string]string{"sha256:aaa": "Jeff's laptop"})

	if len(rows) != 2 {
		t.Fatalf("built %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].name != "This Host" {
		t.Errorf("the first row is %q, want This Host", rows[0].name)
	}
	if rows[0].identity != "" {
		t.Error("this host was made renameable; it is not a peer in the store")
	}
	if rows[1].name != "Jeff's laptop" {
		t.Errorf("the peer shows as %q, not the name it was given", rows[1].name)
	}
	if rows[1].identity != "sha256:aaa" {
		t.Errorf("the peer row carries identity %q, so a rename would go astray",
			rows[1].identity)
	}
	if len(rows[1].children) != 1 || rows[1].children[0].name != "Editor" {
		t.Errorf("the peer's apps read as %+v", rows[1].children)
	}
	if rows[1].children[0].identity != "" {
		t.Error("an app row was made renameable; only a peer has a name to give")
	}
}

// A client nobody has named says so rather than showing a blank first column,
// which would read as a row with nothing in it.
func TestAnUnnamedClientSaysSo(t *testing.T) {
	rows := connectionsRows(storeWith(t, "allow client sha256:aaa"), nil)
	if len(rows) != 2 || rows[1].name != "(unnamed)" {
		t.Errorf("an unnamed client shows as %+v", rows[1:])
	}
}
