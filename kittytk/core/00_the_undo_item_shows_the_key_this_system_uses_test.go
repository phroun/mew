package core

import "testing"

// The system Undo item names trinket_undo and may also show the key for
// trinket_simple_undo; which one it shows is the keymap's own ranking, asked
// across the two. On a Mac the Command key wins, by its hint; everywhere else
// ^Z wins, being written after ^_.
func TestTheUndoItemShowsTheKeyThisSystemUses(t *testing.T) {
	for _, c := range []struct {
		os   string
		want string
	}{
		{"darwin", "s-z"},
		{"linux", "^Z"},
		{"windows", "^Z"},
	} {
		withEnvironment(t, KeymapEnvironment{OS: c.os}, func() {
			r := NewKeyRegistry("t", ParseKeymap(DefaultKeymapConfig))
			undo, simple := r.KeyForCommand(CmdTrinketUndo), r.KeyForCommand(CmdTrinketSimpleUndo)
			got := undo
			if r.Outranks(simple, CmdTrinketSimpleUndo, undo, CmdTrinketUndo) {
				got = simple
			}
			if got != c.want {
				t.Errorf("%s: the Undo item shows %q (undo %q, simple undo %q), want %q",
					c.os, got, undo, simple, c.want)
			}
		})
	}
}

// Outranks compares two bindings by the environment first and then by which was
// bound last, and ranks a pair that is not bound below one that is.
func TestOutranksComparesAcrossCommands(t *testing.T) {
	withEnvironment(t, KeymapEnvironment{OS: "linux"}, func() {
		r := NewKeyRegistry("t", ParseKeymap("^A = one\n^B = two\n(mac) s-a = one\n"))
		if !r.Outranks("^B", "two", "^A", "one") {
			t.Error("the later binding should outrank the earlier")
		}
		if r.Outranks("^A", "one", "^B", "two") {
			t.Error("the earlier binding outranked the later")
		}
		if !r.Outranks("^A", "one", "s-a", "one") {
			t.Error("an unhinted binding should outrank one hinted for another system")
		}
		if r.Outranks("^Q", "one", "^A", "one") {
			t.Error("a pair that is not bound outranked one that is")
		}
		if !r.Outranks("^A", "one", "^Q", "one") {
			t.Error("a bound pair should outrank one that is not")
		}
	})
}
