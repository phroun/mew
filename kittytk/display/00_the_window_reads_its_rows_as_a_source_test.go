package display

// The Connections window with its rows as a SOURCE.
//
// The forty-odd tests around this file were written against the window that wrote
// its rows out as protocol text and hung a `*connectionsRow` on each item. They
// all still pass, which is the claim: what the window shows, what the pane
// answers, what a click on Forget or Clear Cache does. So what is here is only
// what was not possible before -- a column sort reaching a declared tree -- and
// the two seams that carry the rest.

import (
	"path/filepath"
	"testing"
)

// The data columns, in the order the shell script declares them. Two of them are
// never drawn, and the sort is what they exist for.
const (
	lastSeenColumn = 0
	storageColumn  = 1
)

// twoClients is two clients with different amounts of material, so a sort on the
// size has something to say.
func twoClients(t *testing.T) *connectionsView {
	t.Helper()
	known := tempKnown(t)
	if err := known.admitted("sha256:aaa", "", "Alpha", noon); err != nil {
		t.Fatal(err)
	}
	if err := known.admitted("sha256:bbb", "", "Beta", noon); err != nil {
		t.Fatal(err)
	}
	root := configDir()
	put(t, filepath.Join(root, "data", "alpha", "small"), 1024)
	put(t, filepath.Join(root, "data", "beta", "large"), 8192)

	// The nickname is the user's own word and lives in its own store; what
	// `admitted` was given is the folder the material sits in.
	nicks := tempNicknames(t)
	if err := nicks.set("sha256:aaa", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := nicks.set("sha256:bbb", "Beta"); err != nil {
		t.Fatal(err)
	}
	return paneFor(t, storeWith(t), nicks, known)
}

func captionsOf(v *connectionsView) []string {
	out := make([]string, 0, len(v.tree.RootItems()))
	for _, item := range v.tree.RootItems() {
		out = append(out, item.Text)
	}
	return out
}

// **A column sort reaches a DECLARED tree**, which it could not before: a tree
// reading somebody else's rows cannot sort them itself -- the pre-order is built
// rather than sorted -- so the order is told to the source, level by level, and
// the mapping is what translates the column into a field.
//
// The Storage column hands its sorting to the hidden Bytes column, so this also
// says the proxy still resolves first and the mapping second.
func TestSortingAColumnOrdersTheDeclaredLevels(t *testing.T) {
	v := twoClients(t)
	if got, want := captionsOf(v), []string{"This Host", "Alpha", "Beta"}; !same(got, want) {
		t.Fatalf("unsorted the window reads\n  %v\nwant\n  %v", got, want)
	}

	v.tree.SetSorted(true, storageColumn, true)
	got := captionsOf(v)
	if want := []string{"Beta", "Alpha", "This Host"}; !same(got, want) {
		t.Fatalf("by size, largest first, the window reads\n  %v\nwant\n  %v", got, want)
	}

	v.tree.SetSorted(true, storageColumn, false)
	if got, want := captionsOf(v), []string{"This Host", "Alpha", "Beta"}; !same(got, want) {
		t.Errorf("by size, smallest first, the window reads\n  %v\nwant\n  %v", got, want)
	}
}

// **The store's order comes back when the sort is turned off.** It is not any
// field's order -- this host, then the clients there is a rule about, then the ones
// merely admitted -- so `seq` is what carries it, and turning the sort off has to
// give it back rather than leaving the last thing anybody clicked in force.
func TestTurningTheSortOffGivesTheStoresOrderBack(t *testing.T) {
	v := twoClients(t)
	v.tree.SetSorted(true, storageColumn, true)
	if got := captionsOf(v); got[0] != "Beta" {
		t.Fatalf("sorted the window reads %v", got)
	}
	v.tree.SetSorted(false, storageColumn, false)
	if got, want := captionsOf(v), []string{"This Host", "Alpha", "Beta"}; !same(got, want) {
		t.Errorf("with the sort off the window reads\n  %v\nwant\n  %v", got, want)
	}
}

// A sort on a column every row is empty in is still an ORDER: the rows are equal
// on that level, and the identity settles what a sort leaves equal, so every row
// is still there and drawn once. This host's Last Seen cell is the empty one --
// it has not connected to itself -- and it must not vanish or double.
func TestSortingOnAColumnNobodyFillsLosesNoRow(t *testing.T) {
	v := twoClients(t)
	v.tree.SetSorted(true, lastSeenColumn, false)
	if got := captionsOf(v); len(got) != 3 {
		t.Fatalf("sorted on the date the window reads %v, want three rows", got)
	}
	seen := map[string]bool{}
	for _, name := range captionsOf(v) {
		if seen[name] {
			t.Errorf("%q is drawn twice", name)
		}
		seen[name] = true
	}
	for _, name := range []string{"This Host", "Alpha", "Beta"} {
		if !seen[name] {
			t.Errorf("%q went missing when the date was sorted on", name)
		}
	}
}

// **A row leads back to what it is about by its KEY.** The items are the tree's
// own now, made to draw the source's rows, so there was never a moment for the
// window to hang a `*connectionsRow` on one -- and a row that comes and goes as a
// subtree closes and opens would need it hung again each time.
func TestARowLeadsBackToItsClientByItsKey(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	v := paneFor(t, storeWith(t, "deny app sha256:aaa Demo"), tempNicknames(t), known)

	items := v.tree.RootItems()
	if len(items) != 2 || len(items[1].Children) != 1 {
		t.Fatalf("the window reads %v", captionsOf(v))
	}
	peer, app := items[1], items[1].Children[0]
	if peer.Key() == "" || app.Key() == "" {
		t.Fatal("a row out of the source has no key, so nothing leads back to it")
	}
	if peer.Key() == app.Key() {
		t.Error("the client and its app have one key between them")
	}

	if row := v.rowOf(peer); row == nil || row.identity != "sha256:aaa" || row.app != "" {
		t.Errorf("the client row leads to %v", row)
	}
	if row := v.rowOf(app); row == nil || row.app != "Demo" {
		t.Errorf("the app row leads to %v", row)
	}
	// And an item that came from nowhere leads nowhere, rather than to the first
	// row in the list.
	if row := v.rowOf(nil); row != nil {
		t.Errorf("a row that is not there leads to %v", row)
	}
}

// **The same row keeps its pointer when the records are restated**, which is what
// the selection and the row editor hold. Restating the sequence instead would hand
// back different objects and the cursor would land somewhere else.
func TestRestatingTheRecordsKeepsTheRowsPointers(t *testing.T) {
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	v := paneFor(t, storeWith(t), tempNicknames(t), known)

	was := v.tree.RootItems()[1]
	selectRow(t, v, 1, "")
	v.choose(0) // Blocked, which rewrites the store and restates the rows

	now := v.tree.RootItems()[1]
	if now != was {
		t.Error("the client became a different object when its standing changed")
	}
	if got := now.Value("permission"); got != hostChoices[0] {
		t.Errorf("its Permission cell reads %q, want %q", got, hostChoices[0])
	}
	if v.tree.CurrentItem() != was {
		t.Error("the cursor left the row the standing was chosen for")
	}
}

func same(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// **Two clients each presenting an Editor are two rows.** An app's name alone is
// not an identity: keyed by the name, they would be one record, and the tree would
// draw one of them twice and the other not at all.
func TestTwoClientsPresentingTheSameAppAreTwoRows(t *testing.T) {
	store := storeWith(t,
		"allow app sha256:aaa Editor",
		"allow app sha256:bbb Editor",
	)
	v := paneFor(t, store, tempNicknames(t), tempKnown(t))

	items := v.tree.RootItems()
	if len(items) != 3 {
		t.Fatalf("the window reads %v, want this host and two clients", captionsOf(v))
	}
	first, second := items[1].Children, items[2].Children
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("the clients hold %d and %d apps, want one each -- their Editors "+
			"were made one row", len(first), len(second))
	}
	if first[0] == second[0] {
		t.Fatal("both clients' Editors are the same item")
	}
	one, two := v.rowOf(first[0]), v.rowOf(second[0])
	if one == nil || two == nil {
		t.Fatal("an app row leads nowhere")
	}
	if one.identity == two.identity {
		t.Errorf("both Editors belong to %q; each belongs to the client that "+
			"presented it", one.identity)
	}
}

// **The store's order is not any field's order**, so `seq` is what carries it: this
// host first, then the clients there is a rule about in the order they were decided
// about, then the ones merely admitted.
//
// A fingerprint that sorts the other way is what shows it -- without `seq`, the
// identity is what settles a level whose rows are otherwise equal, and the window
// would read in fingerprint order instead of the store's.
func TestTheStoresOrderIsWhatTheWindowReadsIn(t *testing.T) {
	// Decided about in this order, which is the reverse of their fingerprints'.
	store := storeWith(t, "allow client sha256:ccc", "deny client sha256:aaa")
	nicks := tempNicknames(t)
	if err := nicks.set("sha256:ccc", "Decided First"); err != nil {
		t.Fatal(err)
	}
	if err := nicks.set("sha256:aaa", "Decided Second"); err != nil {
		t.Fatal(err)
	}
	v := paneFor(t, store, nicks, tempKnown(t))

	got := captionsOf(v)
	want := []string{"This Host", "Decided First", "Decided Second"}
	if !same(got, want) {
		t.Errorf("the window reads\n  %v\nwant\n  %v -- the order the store put "+
			"them in, not their fingerprints'", got, want)
	}
}
