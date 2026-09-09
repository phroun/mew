package display

// Being let in is what puts a client in the list. The authorizations store
// holds only rules, and a client admitted once has no rule -- so without this
// it would draw on the desktop and appear nowhere.

import (
	"path/filepath"
	"testing"
	"time"
)

// tempKnown is a store in a config directory of this test's own, so the folder
// names it hands out are decided by what this test put on disk and not by
// whatever the machine running it happens to hold.
func tempKnown(t *testing.T) *knownStore {
	t.Helper()
	return newKnownStore(filepath.Join(tempConfig(t), "known"))
}

var noon = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

// find is the record for one host, or for one app of it.
func find(t *testing.T, s *knownStore, identity, app string) knownRecord {
	t.Helper()
	for _, r := range s.all() {
		if r.identity == identity && r.app == app {
			return r
		}
	}
	t.Fatalf("nothing is held for identity %q app %q; the records are %+v",
		identity, app, s.all())
	return knownRecord{}
}

// One admission makes two records: the client, and the app it came as. Both are
// dated, because both are things the window shows a date for.
func TestBeingAdmittedRecordsTheClientAndTheApp(t *testing.T) {
	s := tempKnown(t)
	if err := s.admitted("sha256:aaa", "KittyTK Demo", "Jeff's Laptop", noon); err != nil {
		t.Fatal(err)
	}

	host := find(t, s, "sha256:aaa", "")
	if host.safe != "jeff-s-laptop" {
		t.Errorf("the client is filed under %q", host.safe)
	}
	if seenDate(host.seen) != noon.Local().Format("2006-01-02") {
		t.Errorf("the client was dated %q", host.seen)
	}

	app := find(t, s, "sha256:aaa", "KittyTK Demo")
	if app.safe != "kittytk-demo" {
		t.Errorf("the app is filed under %q", app.safe)
	}
	// The date is the app's own, not read off the client it came in under:
	// two apps of one client are used at different times.
	if seenDate(app.seen) != noon.Local().Format("2006-01-02") {
		t.Errorf("the app was dated %q", app.seen)
	}
}

// Coming back moves the date rather than adding a second line, and does not
// re-file anything: the folder a client's material sits in keeps its name for
// as long as the client is known.
func TestComingBackMovesTheDateAndKeepsTheName(t *testing.T) {
	s := tempKnown(t)
	later := noon.Add(72 * time.Hour)
	for _, at := range []time.Time{noon, later} {
		if err := s.admitted("sha256:aaa", "Demo", "Laptop", at); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.all()); n != 2 {
		t.Errorf("two visits by one client left %d records: %+v", n, s.all())
	}
	if got, want := seenDate(find(t, s, "sha256:aaa", "").seen), later.Local().Format("2006-01-02"); got != want {
		t.Errorf("the client is dated %q, want %q", got, want)
	}
	if got := find(t, s, "sha256:aaa", "Demo").safe; got != "demo" {
		t.Errorf("the app was re-filed as %q on its second visit", got)
	}
}

// A folder name has to be unique among the folders beside it. Two clients whose
// nicknames clean to the same word are told apart; two apps of one client are
// told apart from each other.
func TestNamesAreToldApartWhereTheFoldersSit(t *testing.T) {
	s := tempKnown(t)
	for _, c := range []struct{ identity, app, nick string }{
		{"sha256:aaa", "The Demo", "Jeff's Laptop"},
		{"sha256:bbb", "The Demo", "Jeff's  laptop!"},
		{"sha256:aaa", "the-demo", "Jeff's Laptop"},
	} {
		if err := s.admitted(c.identity, c.app, c.nick, noon); err != nil {
			t.Fatal(err)
		}
	}

	if a, b := find(t, s, "sha256:aaa", "").safe, find(t, s, "sha256:bbb", "").safe; a == b {
		t.Errorf("both clients are filed under %q, so they share one folder", a)
	}
	// Apps are named within their own client's folder, so the same app name
	// under a different client is not a collision and is not numbered.
	if got := find(t, s, "sha256:bbb", "The Demo").safe; got != "the-demo" {
		t.Errorf("another client's app is filed under %q", got)
	}
	if got := find(t, s, "sha256:aaa", "the-demo").safe; got != "the-demo-2" {
		t.Errorf("a second app of one client is filed under %q", got)
	}
}

// Forgetting a client drops what this desktop knows about it and leaves its
// material where it lies, so a folder can be standing that no record mentions.
// The next client whose nickname cleans the same way is filed elsewhere: handed
// that folder, it would open somebody else's material as its own.
func TestAFolderNoRecordMentionsIsStillSpokenFor(t *testing.T) {
	s := tempKnown(t)
	put(t, filepath.Join(configDir(), "cache", "laptop", "leftovers"), 16)

	if err := s.admitted("sha256:aaa", "Demo", "Laptop", noon); err != nil {
		t.Fatal(err)
	}
	if got := find(t, s, "sha256:aaa", "").safe; got != "laptop-2" {
		t.Errorf("the client is filed under %q, which is a folder already holding "+
			"a forgotten client's material", got)
	}

	// The same one folder down: an app is named among the folders inside its
	// own client's, which is where its own would go. The app just recorded is
	// forgotten, leaving its folder, and another arrives whose name cleans the
	// same way.
	put(t, filepath.Join(configDir(), "data", "laptop-2", "demo", "leftovers"), 16)
	if err := s.forgetApp("sha256:aaa", "Demo"); err != nil {
		t.Fatal(err)
	}
	if err := s.admitted("sha256:aaa", "DEMO!", "Laptop", noon); err != nil {
		t.Fatal(err)
	}
	if got := find(t, s, "sha256:aaa", "DEMO!").safe; got != "demo-2" {
		t.Errorf("the app is filed under %q, which is a folder already holding a "+
			"forgotten app's material", got)
	}
}

// A peer with no identity is not recorded. Over a unix socket there is nobody
// to name -- the peer is this machine.
func TestAnAnonymousPeerIsNotRecorded(t *testing.T) {
	s := tempKnown(t)
	if err := s.admitted("", "Demo", "", noon); err != nil {
		t.Fatal(err)
	}
	if n := len(s.all()); n != 0 {
		t.Errorf("a peer with no identity left %d records", n)
	}
}

// A client that connects without naming an app leaves the client's record and
// nothing beneath it.
func TestAClientWithNoAppNamesOnlyItself(t *testing.T) {
	s := tempKnown(t)
	if err := s.admitted("sha256:aaa", "", "Laptop", noon); err != nil {
		t.Fatal(err)
	}
	if n := len(s.all()); n != 1 {
		t.Errorf("a client that named no app left %d records: %+v", n, s.all())
	}
}

// A new nickname is a new folder name, and the store says what the folder was
// called so the folder itself can be moved to match.
func TestRenamingAClientAnswersWithBothNames(t *testing.T) {
	s := tempKnown(t)
	if err := s.admitted("sha256:aaa", "Demo", "Old Name", noon); err != nil {
		t.Fatal(err)
	}

	from, to, err := s.rename("sha256:aaa", "New Name")
	if err != nil {
		t.Fatal(err)
	}
	if from != "old-name" || to != "new-name" {
		t.Errorf("renaming answered %q -> %q", from, to)
	}
	if got := find(t, s, "sha256:aaa", "").safe; got != "new-name" {
		t.Errorf("the client is filed under %q after the rename", got)
	}
	// The apps beneath it are named within the client's folder, so moving the
	// folder leaves their own names alone.
	if got := find(t, s, "sha256:aaa", "Demo").safe; got != "demo" {
		t.Errorf("the app was re-filed as %q by its client being renamed", got)
	}
}

// A nickname that cleans to what the client is already called leaves it alone.
// Numbering it would move the folder for no reason and leave a `-2` behind that
// nothing ever asked for.
func TestRenamingToTheSameCleanNameChangesNothing(t *testing.T) {
	s := tempKnown(t)
	if err := s.admitted("sha256:aaa", "", "Old Name", noon); err != nil {
		t.Fatal(err)
	}
	from, to, err := s.rename("sha256:aaa", "  OLD   name  ")
	if err != nil {
		t.Fatal(err)
	}
	if from != "old-name" || to != "old-name" {
		t.Errorf("renaming answered %q -> %q, want both old-name", from, to)
	}
}

// A client this desktop has never admitted has no folder, so there is nothing
// to move and nothing to answer with.
func TestRenamingAClientWithNoRecordMovesNothing(t *testing.T) {
	from, to, err := tempKnown(t).rename("sha256:aaa", "Some Name")
	if err != nil {
		t.Fatal(err)
	}
	if from != "" || to != "" {
		t.Errorf("renaming an unknown client answered %q -> %q", from, to)
	}
}

// Forgetting a client takes its apps with it; forgetting one app leaves the
// client and its other apps standing.
func TestForgettingTakesTheRightRecords(t *testing.T) {
	s := tempKnown(t)
	for _, app := range []string{"Editor", "Mailer"} {
		if err := s.admitted("sha256:aaa", app, "Laptop", noon); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.admitted("sha256:bbb", "Editor", "Desktop", noon); err != nil {
		t.Fatal(err)
	}

	if err := s.forgetApp("sha256:aaa", "Editor"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.all()); n != 4 {
		t.Errorf("forgetting one app left %d records: %+v", n, s.all())
	}
	find(t, s, "sha256:aaa", "Mailer")

	if err := s.forget("sha256:aaa"); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.all() {
		if r.identity == "sha256:aaa" {
			t.Errorf("%+v outlived the client it belongs to", r)
		}
	}
	find(t, s, "sha256:bbb", "Editor")
}

// What is written comes back: an app name with spaces in it stays whole, and a
// record with no date is not read as one with a date of "-" -- which would put
// a dash in the Last Seen column of every client that has yet to visit.
func TestARecordSurvivesTheFile(t *testing.T) {
	for _, want := range []knownRecord{
		{identity: "sha256:aaa", safe: "jeff-s-laptop", seen: "2026-03-04T12:00:00Z"},
		{identity: "sha256:aaa", safe: "jeff-s-laptop"},
		{identity: "sha256:aaa", app: "The Big  Editor", safe: "the-big-editor", seen: "2026-03-04T12:00:00Z"},
		{identity: "sha256:aaa", app: "Editor", safe: "a-b-c"},
	} {
		s := tempKnown(t)
		s.mu.Lock()
		err := s.writeLocked([]knownRecord{want})
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		got := s.all()
		if len(got) != 1 || got[0] != want {
			t.Errorf("%+v came back as %+v", want, got)
		}
	}
}
