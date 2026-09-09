package display

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
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
	rows := connectionsRows(s, map[string]string{"sha256:aaa": "Jeff's laptop"}, nil)

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
	rows := connectionsRows(storeWith(t, "allow client sha256:aaa"), nil, nil)
	if len(rows) != 2 || rows[1].name != "(unnamed)" {
		t.Errorf("an unnamed client shows as %+v", rows[1:])
	}
}

// Serving is what gives a desktop connections to show, so serving is what
// installs the item -- through any route to a server, not only through the
// convenience config a host is free not to use.
func TestServingInstallsTheItem(t *testing.T) {
	d := trinkets.NewDesktop()
	if d.ConnectionsOpener() != nil {
		t.Fatal("a desktop nobody is serving for offered the item")
	}

	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := ServeConfig(d, Config{Endpoint: sock})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if d.ConnectionsOpener() == nil {
		t.Error("a served desktop has connections to show and no way to show them")
	}
}

// The window opens. Every failure inside showConnections is silent -- the menu
// item simply does nothing -- so the only way to know it works is to build one
// and look at what came back.
//
// It failed here on the protocol registry's own wrappers: a column comes back
// as an unexported *wireColumn and an item as a *wireItem, so asking for either
// by type never succeeded and the guard rejected a window that was fine.
func TestTheWindowActuallyOpens(t *testing.T) {
	d := shownDesktop(t)
	store := storeWith(t, "allow app sha256:aaa Editor", "allow client sha256:bbb")
	nicks := tempNicknames(t)
	if err := nicks.set("sha256:aaa", "the laptop"); err != nil {
		t.Fatal(err)
	}

	win := showConnections(d, store, nicks, tempSeen(t))
	if win == nil {
		t.Fatal("the window did not open, so choosing the menu item does nothing")
	}
	if got := win.Title(); got != "Connections" {
		t.Errorf("opened a window titled %q", got)
	}

	// And it is on the desktop, not merely constructed.
	var found bool
	for _, w := range d.WindowManager().Windows() {
		if w == win {
			found = true
		}
	}
	if !found {
		t.Error("the window was built but never shown")
	}
}

// The short columns are held still and the fingerprint travels. Squeezing them
// all into the window cuts the fingerprint anyway -- it is seventy-one
// characters -- and takes the cut out of the name and the date as well, so the
// tree is put in scroll mode with those two pinned outside the scrolling
// region.
func TestTheFingerprintScrollsAndTheNameStaysPut(t *testing.T) {
	d := shownDesktop(t)
	store := storeWith(t, "allow app sha256:aaa Editor")
	if win := showConnections(d, store, tempNicknames(t), tempSeen(t)); win == nil {
		t.Fatal("the window did not open")
	}

	// The tree the window is actually showing, not the text it was built from.
	tree := openConnectionsTree(t, d)
	if tree.FitWidth() {
		t.Error("the columns are squeezed into the window, so the fingerprint is " +
			"cut short and takes the name's room with it")
	}
	if begin, _ := tree.FixedColumns(); begin != pinnedColumns {
		t.Errorf("%d columns are pinned, want %d; the name and the date lead the "+
			"run and must stay put while the fingerprint travels",
			begin, pinnedColumns)
	}
	script := connectionsShellScript()
	for _, w := range []int{nicknameWidth, lastSeenWidth, identityWidth} {
		if !strings.Contains(script, "width="+strconv.Itoa(w)) {
			t.Errorf("no column is %d units wide, so the window is not built with "+
				"the widths that were reasoned about:\n%s", w, script)
		}
	}
}

// shownDesktop is a desktop something can be shown on. A backend is what gives
// a desktop its window manager, and without one nothing opens at all -- so a
// check that skipped this would pass on a desktop that can show nothing.
func shownDesktop(t *testing.T) *trinkets.Desktop {
	t.Helper()
	d := trinkets.NewDesktop()
	b, err := raster.NewScaled(800, 400, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 8000, Height: 4000})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 8000, Height: 4000})
	return d
}

// openConnectionsTree finds the open window and hands back its tree.
func openConnectionsTree(t *testing.T, d *trinkets.Desktop) *trinkets.TreeView {
	t.Helper()
	for _, w := range d.WindowManager().Windows() {
		if w.Title() != "Connections" {
			continue
		}
		var found *trinkets.TreeView
		var walk func(core.Trinket)
		walk = func(n core.Trinket) {
			if tv, ok := n.(*trinkets.TreeView); ok {
				found = tv
				return
			}
			if box, ok := n.(core.Container); ok {
				for _, c := range box.Children() {
					walk(c)
				}
			}
		}
		walk(w)
		if found != nil {
			return found
		}
	}
	t.Fatal("the window has no tree in it")
	return nil
}
