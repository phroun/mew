package display

// The three columns a row answers about itself without being selected: where it
// stands, what it is taking up, and -- for a client with neither rule nor
// visit -- that it is in the window at all.

import (
	"os"
	"path/filepath"
	"testing"
)

// admittedOnce is a client let in for one session and nothing more: no rule
// written, one record of having been here.
func admittedOnce(t *testing.T, identity, app, nickname string) *knownStore {
	t.Helper()
	s := tempKnown(t)
	if err := s.admitted(identity, app, nickname, noon); err != nil {
		t.Fatal(err)
	}
	return s
}

// A client allowed for one session leaves no rule behind it, so the
// authorizations store says nothing about it. It has still been here, and it is
// still in the window -- with its app beneath it.
func TestAClientAllowedOnceIsStillListed(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "KittyTK Demo", "The Laptop")
	store := storeWith(t)
	if e := store.entries(); len(e) != 0 {
		t.Fatalf("the rules store holds %+v; this test is about a client with none", e)
	}

	rows := connectionsRows(store, map[string]string{"sha256:aaa": "The Laptop"}, known.all())
	if len(rows) != 2 {
		t.Fatalf("built %d rows, want this host and the client that visited: %+v",
			len(rows), rows)
	}
	if rows[1].identity != "sha256:aaa" {
		t.Errorf("the second row is about %q", rows[1].identity)
	}
	if len(rows[1].children) != 1 || rows[1].children[0].app != "KittyTK Demo" {
		t.Errorf("the client's apps read as %+v", rows[1].children)
	}
	// And with no rule of its own it stands where an undecided client stands:
	// it will be asked about again.
	if got := permWord(rows[1]); got != hostChoices[1] {
		t.Errorf("a client with no rule reads as %q", got)
	}
	if got := permWord(rows[1].children[0]); got != appChoices[1] {
		t.Errorf("an app with no rule reads as %q", got)
	}
}

// A client with a rule and no visit, and one with a visit and no rule, are one
// row each rather than one row between them or two rows for the same client.
func TestAClientInBothStoresIsOneRow(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Editor", "")
	rows := connectionsRows(
		storeWith(t, "allow app sha256:aaa Mailer", "deny client sha256:bbb"),
		nil, known.all())

	if len(rows) != 3 {
		t.Fatalf("built %d rows, want this host and two clients: %+v", len(rows), rows)
	}
	if rows[1].identity != "sha256:aaa" || rows[2].identity != "sha256:bbb" {
		t.Errorf("the clients read as %q and %q", rows[1].identity, rows[2].identity)
	}
	// The app it was ruled about and the app it merely arrived as are both
	// beneath it, in name order.
	var apps []string
	for _, c := range rows[1].children {
		apps = append(apps, c.app)
	}
	if len(apps) != 2 || apps[0] != "Editor" || apps[1] != "Mailer" {
		t.Errorf("the client's apps read as %v, want Editor and Mailer", apps)
	}
}

// The Permission column says where a row stands, in the words the pane below
// offers for it -- a client and an app being asked different questions.
func TestThePermissionColumnSaysWhereARowStands(t *testing.T) {
	tempConfig(t)
	rows := connectionsRows(storeWith(t,
		"deny client sha256:aaa",
		"allow app sha256:aaa Editor",
		"allow client sha256:bbb",
		"deny app sha256:bbb Scratch",
	), nil, nil)

	for _, c := range []struct {
		row  connectionsRow
		want string
	}{
		{rows[0], ""},
		{rows[1], hostChoices[0]},
		{rows[1].children[0], appChoices[2]},
		{rows[2], hostChoices[2]},
		{rows[2].children[0], appChoices[0]},
	} {
		if got := permWord(c.row); got != c.want {
			t.Errorf("%q reads as %q, want %q", c.row.name, got, c.want)
		}
	}
}

// An app has no identity of its own: it is trusted through the client that
// presented it, and repeating that client's fingerprint on every app row says
// something about the app that is not true of it.
func TestAnAppHasNoFingerprintOfItsOwn(t *testing.T) {
	tempConfig(t)
	rows := connectionsRows(storeWith(t, "allow app sha256:aaa Editor"), nil, nil)
	if got := identityWord(rows[1]); got != "sha256:aaa" {
		t.Errorf("the client's Identity cell reads %q", got)
	}
	if got := identityWord(rows[1].children[0]); got != "" {
		t.Errorf("the app's Identity cell reads %q", got)
	}
}

// The Storage column is what the row is taking up across both trees. A client
// counts its apps, since their folders sit inside its own.
func TestTheStorageColumnAddsUpBothTrees(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	root := configDir()
	put(t, filepath.Join(root, "data", "laptop", "demo", "bundle"), 2048)
	put(t, filepath.Join(root, "cache", "laptop", "demo", "scratch"), 1024)

	rows := connectionsRows(storeWith(t), nil, known.all())
	if got := storageWord(rows[1]); got != "3.0K" {
		t.Errorf("the client is shown as taking up %q, want 3.0K", got)
	}
	if got := storageWord(rows[1].children[0]); got != "3.0K" {
		t.Errorf("the app is shown as taking up %q, want 3.0K", got)
	}
	if got := storageWord(rows[0]); got != "" {
		t.Errorf("this host is shown as taking up %q; it stores nothing for itself", got)
	}
}

// Clear Cache clears the row it was pressed on, leaves the data tree standing,
// and puts the new figure in the cells that changed -- the app's own, and the
// client's, which counts it.
func TestClearCacheEmptiesTheRowItWasPressedOn(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	root := configDir()
	keep := filepath.Join(root, "data", "laptop", "demo", "bundle")
	put(t, keep, 2048)
	put(t, filepath.Join(root, "cache", "laptop", "demo", "scratch"), 1024)

	v := paneFor(t, storeWith(t), tempNicknames(t), known)
	selectRow(t, v, 1, "Demo")
	v.clear.Click()

	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the app's data went with its cache: %v", err)
	}
	items := v.tree.RootItems()
	if got := items[1].Children[0].Value("storage"); got != "2.0K" {
		t.Errorf("the app's Storage cell reads %q after its cache was cleared", got)
	}
	if got := items[1].Value("storage"); got != "2.0K" {
		t.Errorf("the client's Storage cell reads %q; it counts the app whose "+
			"cache just went", got)
	}
}

// This host stores nothing for itself and has nothing to clear, so the button
// does nothing rather than reaching for a folder named after a row that has no
// name on disk.
func TestClearCacheDoesNothingToThisHost(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	put(t, filepath.Join(configDir(), "cache", "laptop", "scratch"), 1024)

	v := paneFor(t, storeWith(t), tempNicknames(t), known)
	selectRow(t, v, 0, "")
	v.clear.Click()

	if got := storageUsed("laptop", ""); got != 1024 {
		t.Errorf("a client's cache went when the button was pressed on this host: "+
			"%d bytes left, want 1024", got)
	}
}

// Renaming a client moves the folder its material sits in, so what it had under
// the old name it still has under the new one -- and the row goes on reading
// the right figure.
func TestRenamingAClientCarriesItsMaterialWithIt(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Old Name")
	root := configDir()
	put(t, filepath.Join(root, "data", "old-name", "demo", "bundle"), 2048)
	put(t, filepath.Join(root, "cache", "old-name", "scratch"), 1024)

	nicks := tempNicknames(t)
	if err := nicks.set("sha256:aaa", "Old Name"); err != nil {
		t.Fatal(err)
	}
	v := paneFor(t, storeWith(t), nicks, known)
	selectRow(t, v, 1, "")
	v.rename(v.tree.RootItems()[1], "New Name")

	if _, err := os.Stat(filepath.Join(root, "data", "new-name", "demo", "bundle")); err != nil {
		t.Errorf("the client's material did not move with it: %v", err)
	}
	if got := hostSafe(known.all(), "sha256:aaa"); got != "new-name" {
		t.Errorf("the client is filed under %q after the rename", got)
	}
	row := rowOf(v.tree.RootItems()[1])
	if row.hostSafe != "new-name" {
		t.Errorf("the row still reads from %q", row.hostSafe)
	}
	if got := storageWord(*row); got != "3.0K" {
		t.Errorf("the renamed client is shown as taking up %q, want 3.0K", got)
	}
	if got := storageWord(row.children[0]); got != "2.0K" {
		t.Errorf("the app under the renamed client is shown as taking up %q", got)
	}
}
