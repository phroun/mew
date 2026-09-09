package display

// The pane under the list: what it says about the current row, and what
// choosing in it does to the store behind that row.

import (
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/trinkets"
)

// paneFor opens the window over the given stores and hands back the view, with
// the tree already holding this host and whatever the store decided.
func paneFor(t *testing.T, store *authStore, nicks, seen *pairStore) *connectionsView {
	t.Helper()
	return paneWithHost(t, nil, store, nicks, seen)
}

// paneWithHost is the same with a server behind it, which is what the two
// switches above the list are for.
func paneWithHost(t *testing.T, host connectionsHost, store *authStore, nicks, seen *pairStore) *connectionsView {
	t.Helper()
	v, win := buildConnections(shownDesktop(t), host, store, nicks, seen)
	if v == nil || win == nil {
		t.Fatal("the window did not build, so the menu item does nothing")
	}
	return v
}

// selectRow puts the cursor on a top row, or on one of its apps when app is
// given, and returns nothing -- what it did is read off the pane.
func selectRow(t *testing.T, v *connectionsView, at int, app string) {
	t.Helper()
	items := v.tree.RootItems()
	if at >= len(items) {
		t.Fatalf("the tree has %d rows; asked for row %d", len(items), at)
	}
	item := items[at]
	if app != "" {
		found := false
		for _, c := range item.Children {
			if c.Text == app {
				item, found = c, true
				break
			}
		}
		if !found {
			t.Fatalf("row %d has no app called %q", at, app)
		}
	}
	v.tree.SetCurrentItem(item)
	if v.tree.CurrentItem() != item {
		t.Fatal("the row did not become current")
	}
}

func choiceCaptions(v *connectionsView) [3]string {
	var out [3]string
	for i, c := range v.choices {
		out[i] = c.Text()
	}
	return out
}

func checkedChoice(t *testing.T, v *connectionsView) int {
	t.Helper()
	at := -1
	for i, c := range v.choices {
		if c.IsChecked() {
			if at >= 0 {
				t.Fatalf("choices %d and %d are both checked", at, i)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatal("no standing is checked, so the pane says nothing about this row")
	}
	return at
}

// The window is three bands down the page: a pane, the list, and the pane that
// answers whichever row the list is on. The list is the one that grows, so the
// panes keep their size whatever the window is given.
func TestTheListSitsBetweenTwoPanes(t *testing.T) {
	_, win := buildConnections(shownDesktop(t), nil, storeWith(t), tempNicknames(t), tempSeen(t))
	if win == nil {
		t.Fatal("the window did not build")
	}
	root, ok := win.Children()[0].(*trinkets.Panel)
	if !ok {
		t.Fatalf("the window holds a %T, want a panel", win.Children()[0])
	}
	if _, ok := root.LayoutManager().(*layout.BoxLayout); !ok {
		t.Fatalf("the window's panel lays out with a %T, want a box",
			root.LayoutManager())
	}

	kids := root.Children()
	if len(kids) != 3 {
		t.Fatalf("the window has %d bands, want a pane, the list, and a pane: %T",
			len(kids), kids)
	}
	if _, ok := kids[0].(*trinkets.Panel); !ok {
		t.Errorf("the band above the list is a %T, want a panel", kids[0])
	}
	tree, ok := kids[1].(*trinkets.TreeView)
	if !ok {
		t.Fatalf("the middle band is a %T, want the list", kids[1])
	}
	if s, set := tree.LayoutStretchHint(); !set || s < 1 {
		t.Error("the list does not take the leftover height, so a taller window " +
			"grows the panes instead of the list")
	}
	below, ok := kids[2].(*trinkets.Panel)
	if !ok {
		t.Fatalf("the band below the list is a %T, want a panel", kids[2])
	}
	if _, ok := below.LayoutManager().(*layout.GridLayout); !ok {
		t.Errorf("the pane below lays out with a %T, want a grid",
			below.LayoutManager())
	}
}

// A client's row: its fingerprint, and the three standings a client can be
// put at, with the one it stands at now.
func TestAClientReadsAsItsFingerprintAndItsStanding(t *testing.T) {
	store := storeWith(t, "deny client sha256:aaa", "allow app sha256:aaa Editor")
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "")

	if got := v.subject.Text(); got != "Identity:" {
		t.Errorf("the pane calls a client %q", got)
	}
	if got := v.value.Text(); got != "sha256:aaa" {
		t.Errorf("the pane shows %q, not the fingerprint the row is about", got)
	}
	if got := choiceCaptions(v); got != hostChoices {
		t.Errorf("a client is offered %v, want %v", got, hostChoices)
	}
	if got := checkedChoice(t, v); got != 0 {
		t.Errorf("a blocked client reads as standing %d (%q), want Blocked",
			got, v.choices[got].Text())
	}
	if !v.permrow.IsVisible() || !v.actrow.IsVisible() {
		t.Error("a client was shown no way to be answered")
	}
}

// An app's row: its name, and the three standings an app can be put at -- the
// middle one being no standing of its own.
func TestAnAppReadsAsItsNameAndItsStanding(t *testing.T) {
	store := storeWith(t, "allow client sha256:aaa", "deny app sha256:aaa Scratch")
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "Scratch")

	if got := v.subject.Text(); got != "App Name:" {
		t.Errorf("the pane calls an app %q", got)
	}
	if got := v.value.Text(); got != "Scratch" {
		t.Errorf("the pane shows %q, not the app the row is about", got)
	}
	if got := choiceCaptions(v); got != appChoices {
		t.Errorf("an app is offered %v, want %v", got, appChoices)
	}
	if got := checkedChoice(t, v); got != 0 {
		t.Errorf("a denied app reads as standing %d (%q), want Deny",
			got, v.choices[got].Text())
	}
}

// An app with no line of its own stands at Follow Host Rule, which is what the
// gate does with it: nothing, and lets the client's own standing answer.
func TestAnAppWithNoLineFollowsItsHost(t *testing.T) {
	store := storeWith(t, "allow app sha256:aaa Editor")
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "Editor")
	if got := checkedChoice(t, v); got != 2 {
		t.Fatalf("an allowed app reads as standing %d", got)
	}

	v.choices[1].SetChecked(true) // Follow Host Rule
	if allow, ok := store.decide(AuthRequest{Fingerprint: "sha256:aaa", AppName: "Editor"}); ok {
		t.Errorf("the store still decides the app (allow=%v); it was asked to "+
			"leave the app to its client", allow)
	}
	selectRow(t, v, 0, "")
	selectRow(t, v, 1, "Editor")
	if got := checkedChoice(t, v); got != 1 {
		t.Errorf("after being set to follow its host the app reads as %d", got)
	}
}

// This host is us: there is no standing to grant ourselves and nothing to
// forget, so the pane shows what we are and stops there.
func TestThisHostIsOnlyShownNotAnswered(t *testing.T) {
	v := paneFor(t, storeWith(t, "allow client sha256:aaa"), tempNicknames(t), tempSeen(t))
	selectRow(t, v, 0, "")

	if got := v.subject.Text(); got != "Identity:" {
		t.Errorf("this host is called %q", got)
	}
	if v.value.Text() == "" {
		t.Error("this host is shown without an identity")
	}
	if v.permrow.IsVisible() || v.permhead.IsVisible() {
		t.Error("this host was offered a permission to grant itself")
	}
	if v.actrow.IsVisible() || v.acthead.IsVisible() {
		t.Error("this host was offered an action -- there is nothing to forget")
	}
}

// Choosing a standing writes it: the store decides the way the pane says, and
// the row's own Identity cell says the same thing.
func TestChoosingAStandingRewritesTheStore(t *testing.T) {
	store := storeWith(t, "allow client sha256:aaa")
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "")
	if got := checkedChoice(t, v); got != 2 {
		t.Fatalf("a client allowed for every app reads as standing %d", got)
	}

	v.choices[0].SetChecked(true) // Blocked
	allow, ok := store.decide(AuthRequest{Fingerprint: "sha256:aaa", AppName: "Editor"})
	if !ok || allow {
		t.Errorf("after Blocked the store decides allow=%v decided=%v", allow, ok)
	}
	if got := v.tree.RootItems()[1].Value("identity"); !strings.Contains(got, "blocked") {
		t.Errorf("the row still reads %q, so the list and the pane disagree", got)
	}

	v.choices[1].SetChecked(true) // Prompt
	if _, ok := store.decide(AuthRequest{Fingerprint: "sha256:aaa", AppName: "Editor"}); ok {
		t.Error("after Prompt the store still decides for the client")
	}
}

// An app is answered by itself: setting one says nothing about its client or
// about the client's other apps.
func TestAnAppsStandingIsItsOwn(t *testing.T) {
	store := storeWith(t,
		"allow client sha256:aaa",
		"allow app sha256:aaa Editor",
		"allow app sha256:aaa Mailer",
	)
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "Editor")
	v.choices[0].SetChecked(true) // Deny

	if allow, ok := store.decide(AuthRequest{Fingerprint: "sha256:aaa", AppName: "Editor"}); !ok || allow {
		t.Errorf("the denied app decides allow=%v decided=%v", allow, ok)
	}
	if allow, ok := store.decide(AuthRequest{Fingerprint: "sha256:aaa", AppName: "Mailer"}); !ok || !allow {
		t.Errorf("the client's other app decides allow=%v decided=%v", allow, ok)
	}
}

// Forget drops a client entirely -- its apps, its name, and when it was last
// here -- and takes its row with it.
func TestForgettingAClientLeavesNothingBehind(t *testing.T) {
	store := storeWith(t, "allow client sha256:aaa", "allow app sha256:aaa Editor")
	nicks, seen := tempNicknames(t), tempSeen(t)
	if err := nicks.set("sha256:aaa", "the laptop"); err != nil {
		t.Fatal(err)
	}
	if err := markSeen(seen, "sha256:aaa", time.Now()); err != nil {
		t.Fatal(err)
	}

	v := paneFor(t, store, nicks, seen)
	selectRow(t, v, 1, "")
	v.forget.Click()

	if e := store.entries(); len(e) != 0 {
		t.Errorf("the store still holds %+v", e)
	}
	if got := nicks.get("sha256:aaa"); got != "" {
		t.Errorf("the name %q outlived the client it named", got)
	}
	if got := seen.get("sha256:aaa"); got != "" {
		t.Errorf("the last-seen stamp %q outlived the client", got)
	}
	if n := len(v.tree.RootItems()); n != 1 {
		t.Errorf("the tree still has %d rows; only this host should be left", n)
	}
}

// Forgetting one app leaves its client, and the client's other apps, alone.
func TestForgettingAnAppLeavesItsClient(t *testing.T) {
	store := storeWith(t,
		"allow client sha256:aaa",
		"allow app sha256:aaa Editor",
		"allow app sha256:aaa Mailer",
	)
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	selectRow(t, v, 1, "Editor")
	v.forget.Click()

	e := store.entries()
	if len(e) != 1 || e[0].identity != "sha256:aaa" {
		t.Fatalf("the client went with its app: %+v", e)
	}
	if len(e[0].apps) != 1 || e[0].apps[0].name != "Mailer" {
		t.Errorf("the client's apps read as %+v, want only Mailer", e[0].apps)
	}
	kids := v.tree.RootItems()[1].Children
	if len(kids) != 1 || kids[0].Text != "Mailer" {
		t.Errorf("the client's rows read as %+v", kids)
	}
}

// Nothing the window does to the store is done by the pane writing itself: the
// radios move whenever a row is selected, and that is not a choice.
func TestSelectingARowDecidesNothing(t *testing.T) {
	store := storeWith(t, "allow client sha256:aaa", "deny client sha256:bbb")
	v := paneFor(t, store, tempNicknames(t), tempSeen(t))
	before := store.entries()

	selectRow(t, v, 1, "")
	selectRow(t, v, 2, "")
	selectRow(t, v, 1, "")

	after := store.entries()
	if len(before) != len(after) {
		t.Fatalf("reading the rows changed the store: %+v -> %+v", before, after)
	}
	for i := range before {
		if before[i].identity != after[i].identity ||
			before[i].allow != after[i].allow || before[i].deny != after[i].deny {
			t.Errorf("row %d changed by being looked at: %+v -> %+v",
				i, before[i], after[i])
		}
	}
}
