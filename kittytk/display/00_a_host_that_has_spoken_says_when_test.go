package display

// The Last Seen column: what a stamp reads as, which rows carry one, and the
// headers that put the list in order -- along with the columns nobody sees that
// hold what those headers actually sort on.

import (
	"path/filepath"
	"testing"
	"time"
)

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

// Every row that has visited carries its own date -- the client, and each app
// beneath it. Two apps of one client are used at different times, and a date
// read off the client would say the same thing about both. This host has no
// visit to record: it is the one being visited.
func TestEveryRowThatHasVisitedCarriesItsOwnDate(t *testing.T) {
	tempConfig(t)
	early := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	late := time.Date(2026, 7, 8, 9, 0, 0, 0, time.UTC)

	rows := connectionsRows(storeWith(t, "allow app sha256:aaa Editor"), nil, []knownRecord{
		{identity: "sha256:aaa", safe: "laptop", seen: late.Format(time.RFC3339)},
		{identity: "sha256:aaa", app: "Editor", safe: "editor", seen: early.Format(time.RFC3339)},
	})

	if len(rows) != 2 {
		t.Fatalf("built %d rows, want 2: %+v", len(rows), rows)
	}
	if seenWord(rows[0]) != "" {
		t.Errorf("this host was given a last-seen date of %q", seenWord(rows[0]))
	}
	if want := late.Local().Format("2006-01-02"); seenWord(rows[1]) != want {
		t.Errorf("the client was last seen %q, want %q", seenWord(rows[1]), want)
	}
	if want := early.Local().Format("2006-01-02"); seenWord(rows[1].children[0]) != want {
		t.Errorf("the app was last seen %q, want %q -- an app carries its own date",
			seenWord(rows[1].children[0]), want)
	}
}

// A client with a rule but no visit -- one blocked before it ever drew anything
// -- shows nothing rather than a made-up day.
func TestAClientWithNoStampShowsNothing(t *testing.T) {
	tempConfig(t)
	rows := connectionsRows(storeWith(t, "deny client sha256:aaa"), nil, nil)
	if seenWord(rows[1]) != "" {
		t.Errorf("a client with no stamp shows %q", seenWord(rows[1]))
	}
}

// And the date arrives in the window's own cells, in its own column, without
// displacing the fingerprint that was already there.
func TestTheDateReachesItsColumn(t *testing.T) {
	tempConfig(t)
	d := shownDesktop(t)
	known := tempKnown(t)
	at := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	if err := known.admitted("sha256:aaa", "Editor", "", at); err != nil {
		t.Fatal(err)
	}

	store := storeWith(t, "allow app sha256:aaa Editor")
	if showConnections(d, nil, store, tempNicknames(t), known) == nil {
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

// The headers sort the list. Which client was here longest ago, and which is
// taking up the most room, are the two questions this window is opened to
// answer, and reading either off an unordered list means reading every row.
//
// Neither sorts on what it SHOWS. A day would put two visits an hour apart at
// the same place, and an abbreviated size sorts as text: 9K after 10M, and 999b
// after 1.0K. Each hands its sorting to a column nobody sees, holding what the
// shown value was abbreviated from.
func TestTheShownColumnsSortOnWhatTheyWereAbbreviatedFrom(t *testing.T) {
	tempConfig(t)
	d := shownDesktop(t)
	if showConnections(d, nil, storeWith(t), tempNicknames(t), tempKnown(t)) == nil {
		t.Fatal("the window did not open")
	}
	columns := openConnectionsTree(t, d).Columns()

	at := map[string]int{}
	for i, col := range columns {
		at[col.ID] = i
	}
	for _, c := range []struct{ shown, hidden string }{
		{"lastseen", "stamp"},
		{"storage", "bytes"},
	} {
		i, ok := at[c.shown]
		if !ok {
			t.Fatalf("the tree has no %s column", c.shown)
		}
		if !columns[i].Sortable {
			t.Errorf("the %s header does nothing when it is clicked", c.shown)
		}
		j, ok := at[c.hidden]
		if !ok {
			t.Fatalf("the tree has no %s column for %s to sort on", c.hidden, c.shown)
		}
		if !columns[j].Hidden {
			t.Errorf("the %s column is drawn; it holds sort values, not anything "+
				"the user reads", c.hidden)
		}
		if columns[i].SortProxy != j {
			t.Errorf("%s sorts on column %d, want %s at %d",
				c.shown, columns[i].SortProxy, c.hidden, j)
		}
	}
	// Sizes compare as numbers. Left as text, 999 sorts after 1024.
	if !columns[at["bytes"]].Numeric {
		t.Error("byte counts sort as text, so 999b comes after 1.0K")
	}
	// The pinning counts along the shown columns, so the hidden pair must not
	// sit among the ones being counted.
	for _, id := range []string{"stamp", "bytes"} {
		if at[id] < pinnedColumns {
			t.Errorf("the hidden %s column sits at %d, inside the %d that are pinned",
				id, at[id], pinnedColumns)
		}
	}
}

// The hidden columns hold the whole of what the shown ones abbreviate: the
// moment, not the day, and the byte count, not the figure.
func TestTheHiddenColumnsHoldTheWholeValue(t *testing.T) {
	d := shownDesktop(t)
	known := tempKnown(t)
	at := time.Date(2026, 3, 4, 22, 15, 30, 0, time.UTC)
	if err := known.admitted("sha256:aaa", "Demo", "Laptop", at); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(configDir(), "data", "laptop", "demo", "bundle"), 3000)

	if showConnections(d, nil, storeWith(t), tempNicknames(t), known) == nil {
		t.Fatal("the window did not open")
	}
	peer := openConnectionsTree(t, d).RootItems()[1]

	if got, want := peer.Value("stamp"), at.Format(time.RFC3339); got != want {
		t.Errorf("the hidden stamp cell reads %q, want %q", got, want)
	}
	if got := peer.Value("bytes"); got != "3000" {
		t.Errorf("the hidden size cell reads %q, want 3000", got)
	}
	// And what is shown is still the abbreviation.
	if got := peer.Value("storage"); got != "2.9K" {
		t.Errorf("the Storage cell reads %q, want 2.9K", got)
	}
}
