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

// The clipboard commands advertise the Command keys on a Mac and the Control
// ones everywhere else, as undo and select-all do.
func TestTheClipboardShowsTheKeysThisSystemUses(t *testing.T) {
	for _, c := range []struct {
		os                  string
		cut, copyKey, paste string
	}{
		{"darwin", "s-x", "s-c", "s-v"},
		{"linux", "^X", "^C", "^V"},
	} {
		withEnvironment(t, KeymapEnvironment{OS: c.os}, func() {
			r := NewKeyRegistry("t", ParseKeymap(DefaultKeymapConfig))
			for cmd, want := range map[string]string{
				CmdTrinketCut: c.cut, CmdTrinketCopy: c.copyKey, CmdTrinketPaste: c.paste,
			} {
				if got := r.KeyForCommand(cmd); got != want {
					t.Errorf("%s: %s shows %q, want %q", c.os, cmd, got, want)
				}
			}
		})
	}
}

// Quit, Hide and Hide Others advertise the Command keys on a Mac, where the
// menu bar shows them, and the Control ones everywhere else.
func TestQuitAndHideShowTheKeysThisSystemUses(t *testing.T) {
	for _, c := range []struct {
		os                     string
		quit, hide, hideOthers string
	}{
		{"darwin", "s-q", "s-h", "M-s-h"},
		{"linux", "^Q", "^H", "M-^H"},
		{"windows", "^Q", "^H", "M-^H"},
	} {
		withEnvironment(t, KeymapEnvironment{OS: c.os}, func() {
			r := NewKeyRegistry("t", ParseKeymap(DefaultKeymapConfig))
			for _, k := range []struct{ command, want string }{
				{CmdAppQuit, c.quit}, {CmdAppHide, c.hide}, {CmdAppHideOthers, c.hideOthers},
			} {
				if got := r.KeyForCommand(k.command); got != k.want {
					t.Errorf("%s: %s shows %q, want %q", c.os, k.command, got, k.want)
				}
			}
		})
	}
}
