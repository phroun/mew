package display

import (
	"path/filepath"
	"testing"
	"time"
)

func tempSeen(t *testing.T) *pairStore {
	t.Helper()
	return newSeenStore(filepath.Join(t.TempDir(), "last_seen"))
}

// A stamp is written when a client is admitted and read back as the day it
// names. The store keeps the whole moment: the window shows a date, but a
// stamp cut down to one cannot be asked anything finer afterwards.
func TestAVisitIsStampedToTheSecondAndShownAsADay(t *testing.T) {
	s := tempSeen(t)
	at := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	if err := markSeen(s, "sha256:aaa", at); err != nil {
		t.Fatal(err)
	}

	stamp := s.get("sha256:aaa")
	if got, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Fatalf("stored %q, which is not a time: %v", stamp, err)
	} else if !got.Equal(at) {
		t.Errorf("stored %v, want %v", got, at)
	}
	if got, want := seenDate(stamp), at.Local().Format("2006-01-02"); got != want {
		t.Errorf("the day reads as %q, want %q", got, want)
	}
}

// Coming back replaces the last visit rather than adding to it: the column
// says when a client was last here, not every time it ever was.
func TestTheLatestVisitIsTheOneKept(t *testing.T) {
	s := tempSeen(t)
	first := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	later := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	if err := markSeen(s, "sha256:aaa", first); err != nil {
		t.Fatal(err)
	}
	if err := markSeen(s, "sha256:aaa", later); err != nil {
		t.Fatal(err)
	}
	if got, want := seenDate(s.get("sha256:aaa")), later.Local().Format("2006-01-02"); got != want {
		t.Errorf("after a second visit the store says %q, want %q", got, want)
	}
	if n := len(s.all()); n != 1 {
		t.Errorf("the store holds %d clients after one client visited twice", n)
	}
}

// A peer with no identity is not written at all. Over a unix socket there is
// nobody to name -- the peer is this machine -- so there is no row to stamp.
func TestAnAnonymousPeerIsNotStamped(t *testing.T) {
	s := tempSeen(t)
	if err := markSeen(s, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := len(s.all()); n != 0 {
		t.Errorf("the store holds %d entries for a peer with no identity", n)
	}
}

// Anything the file holds that is not a time reads as no date. A wrong date is
// worse than a blank one, since the column is what the user judges a stale
// authorization by.
func TestAStampThatCannotBeReadShowsNothing(t *testing.T) {
	for _, junk := range []string{"", "yesterday", "2026-03-04", "0"} {
		if got := seenDate(junk); got != "" {
			t.Errorf("%q read as the date %q", junk, got)
		}
	}
}

// The date reaches the window on the peer's own row. This host is us and has
// no visit to record; an app row is one line of a peer's visit, not a visit of
// its own.
func TestTheDateLandsOnThePeerRow(t *testing.T) {
	s := storeWith(t, "allow app sha256:aaa Editor")
	at := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	rows := connectionsRows(s, nil, map[string]string{
		"sha256:aaa": at.Format(time.RFC3339),
	})

	if len(rows) != 2 {
		t.Fatalf("built %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].seen != "" {
		t.Errorf("this host was given a last-seen date of %q", rows[0].seen)
	}
	if want := at.Local().Format("2006-01-02"); rows[1].seen != want {
		t.Errorf("the peer was last seen %q, want %q", rows[1].seen, want)
	}
	if rows[1].children[0].seen != "" {
		t.Errorf("an app row was given its own date %q", rows[1].children[0].seen)
	}
}

// A client that has never been heard from since the column existed shows
// nothing rather than a made-up day.
func TestAClientWithNoStampShowsNothing(t *testing.T) {
	rows := connectionsRows(storeWith(t, "allow client sha256:aaa"), nil, nil)
	if rows[1].seen != "" {
		t.Errorf("a client with no stamp shows %q", rows[1].seen)
	}
}

// And the date arrives in the window's own cells, in its own column, without
// displacing the fingerprint that was already there.
func TestTheDateReachesItsColumn(t *testing.T) {
	d := shownDesktop(t)
	seen := tempSeen(t)
	at := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	if err := markSeen(seen, "sha256:aaa", at); err != nil {
		t.Fatal(err)
	}

	store := storeWith(t, "allow app sha256:aaa Editor")
	if showConnections(d, nil, store, tempNicknames(t), seen) == nil {
		t.Fatal("the window did not open")
	}

	items := openConnectionsTree(t, d).RootItems()
	if len(items) != 2 {
		t.Fatalf("the tree has %d top rows, want 2", len(items))
	}
	peer := items[1]
	if got, want := peer.Value("lastseen"), at.Local().Format("2006-01-02"); got != want {
		t.Errorf("the peer's Last Seen cell reads %q, want %q", got, want)
	}
	if got := peer.Value("identity"); got != "sha256:aaa" {
		t.Errorf("the peer's Identity cell reads %q; the new column displaced it", got)
	}
	if got := items[0].Value("lastseen"); got != "" {
		t.Errorf("this host's Last Seen cell reads %q", got)
	}
}
