package trinkets

import (
	"reflect"
	"testing"

	"github.com/phroun/kittytk/core"
)

// A title marks its accelerator with &, escapes a literal one as &&, and may
// mark SEVERAL letters — which reads as a preference list rather than a
// mistake. The parser used to keep only the first and discard the rest, so a
// backup letter could be written but never reached.
func TestParseAcceleratorTitleCollectsEveryCandidate(t *testing.T) {
	for _, c := range []struct {
		raw     string
		display string
		accels  []acceleratorCandidate
	}{
		{"&File", "File", []acceleratorCandidate{{'f', 0}}},
		{"E&xit", "Exit", []acceleratorCandidate{{'x', 1}}},
		{"&Hel&p", "Help", []acceleratorCandidate{{'h', 0}, {'p', 3}}},
		{"&A&B&C", "ABC", []acceleratorCandidate{{'a', 0}, {'b', 1}, {'c', 2}}},

		// A literal ampersand marks nothing, and does not swallow the letter
		// after it.
		{"Save && Exit", "Save & Exit", nil},
		{"R&&D &Report", "R&D Report", []acceleratorCandidate{{'r', 4}}},

		// Case is folded for matching but kept for display.
		{"&Window", "Window", []acceleratorCandidate{{'w', 0}}},

		// Degenerate markup: a trailing & is dropped rather than kept.
		{"Help&", "Help", nil},
		{"", "", nil},
	} {
		display, accels := parseAcceleratorTitle(c.raw)
		if display != c.display {
			t.Errorf("%q: display = %q, want %q", c.raw, display, c.display)
		}
		if !reflect.DeepEqual(accels, c.accels) {
			t.Errorf("%q: accels = %v, want %v", c.raw, accels, c.accels)
		}
	}
}

// The leading candidate is what a title means before anything has claimed its
// letters, which is what every caller wants at construction time.
func TestFirstAccelerator(t *testing.T) {
	_, accels := parseAcceleratorTitle("&Hel&p")
	if ch, pos := firstAccelerator(accels); ch != 'h' || pos != 0 {
		t.Errorf("firstAccelerator = (%q, %d), want ('h', 0)", ch, pos)
	}
	if ch, pos := firstAccelerator(nil); ch != 0 || pos != -1 {
		t.Errorf("firstAccelerator(nil) = (%q, %d), want (0, -1)", ch, pos)
	}
}

// Marking several letters must not change what the label reads as: the extra
// markers are instructions to the assigner, not text.
func TestCandidateMarkupIsInvisible(t *testing.T) {
	plain, _ := parseAcceleratorTitle("Help")
	marked, accels := parseAcceleratorTitle("&Hel&p")
	if plain != marked {
		t.Errorf("marking backups changed the display text: %q vs %q", plain, marked)
	}
	if len(accels) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(accels))
	}
	// Each position indexes the display text, not the raw markup.
	runes := []rune(marked)
	for _, a := range accels {
		if a.Pos < 0 || a.Pos >= len(runes) {
			t.Fatalf("candidate %v out of range for %q", a, marked)
		}
		if got := []rune(lowerRune(runes[a.Pos])); got[0] != a.Char {
			t.Errorf("candidate %v points at %q in %q", a, runes[a.Pos], marked)
		}
	}
}

// A menu item and a menu both keep the whole list, so the greedy assignment
// has backups to fall to; the chosen one starts as the first.
func TestItemAndMenuKeepCandidates(t *testing.T) {
	item := NewMenuItem("&Hel&p")
	if len(item.acceleratorCandidates) != 2 {
		t.Errorf("item kept %d candidates, want 2", len(item.acceleratorCandidates))
	}
	if item.acceleratorChar != 'h' || item.acceleratorPos != 0 {
		t.Errorf("item chose (%q, %d), want ('h', 0)", item.acceleratorChar, item.acceleratorPos)
	}

	menu := NewMenu("&Hel&p")
	if len(menu.acceleratorCandidates) != 2 {
		t.Errorf("menu kept %d candidates, want 2", len(menu.acceleratorCandidates))
	}
	if menu.acceleratorChar != 'h' || menu.acceleratorPos != 0 {
		t.Errorf("menu chose (%q, %d), want ('h', 0)", menu.acceleratorChar, menu.acceleratorPos)
	}

	// Renaming re-reads the candidates rather than keeping the old ones.
	item.SetText("&File")
	if len(item.acceleratorCandidates) != 1 || item.acceleratorChar != 'f' {
		t.Errorf("after SetText: %v / %q", item.acceleratorCandidates, item.acceleratorChar)
	}
}

func lowerRune(r rune) string {
	if r >= 'A' && r <= 'Z' {
		return string(r + 32)
	}
	return string(r)
}

// candsOf builds one level's worth of candidate lists from raw titles.
func candsOf(titles ...string) [][]acceleratorCandidate {
	out := make([][]acceleratorCandidate, len(titles))
	for i, t := range titles {
		_, out[i] = parseAcceleratorTitle(t)
	}
	return out
}

// letters renders an assignment compactly: the letter, uppercased when it is
// active, "-" when there is no underline at all.
func letters(as []acceleratorAssignment) string {
	s := ""
	for _, a := range as {
		switch {
		case a.Char == 0:
			s += "-"
		case a.Active:
			s += string(a.Char - 32)
		default:
			s += string(a.Char)
		}
	}
	return s
}

// Four siblings all offering the same three letters take them in turn, and the
// fourth is left without one.
func TestAssignGreedyAcrossSiblings(t *testing.T) {
	got := letters(assignAccelerators(candsOf("&A&B&C", "&A&B&C", "&A&B&C", "&A&B&C"), nil))
	if got != "ABC-" {
		t.Errorf("got %q, want %q", got, "ABC-")
	}
}

// The ordinary case: distinct first letters, everything lit.
func TestAssignDistinctLetters(t *testing.T) {
	got := letters(assignAccelerators(candsOf("&File", "&Edit", "&View", "&Help"), nil))
	if got != "FEVH" {
		t.Errorf("got %q, want %q", got, "FEVH")
	}
}

// A backup letter earns its keep against the CONTEXT, not just against
// siblings: with the chord h would form already claimed, Help falls to p and
// stays reachable.
func TestAssignFallsToBackupOnContextClash(t *testing.T) {
	clash := func(r rune) bool { return r == 'h' }
	got := letters(assignAccelerators(candsOf("&File", "&Hel&p"), clash))
	if got != "FP" {
		t.Errorf("got %q, want %q", got, "FP")
	}
}

// With no backup to fall to, the letter is still this item's — shown muted
// rather than surrendered, so it comes back when the clash goes away.
func TestAssignMutesWhenNoBackup(t *testing.T) {
	clash := func(r rune) bool { return r == 'h' }
	got := letters(assignAccelerators(candsOf("&File", "&Help"), clash))
	if got != "Fh" {
		t.Errorf("got %q, want %q", got, "Fh")
	}
	// ...and the same list un-mutes on its own once nothing claims the chord.
	if got := letters(assignAccelerators(candsOf("&File", "&Help"), nil)); got != "FH" {
		t.Errorf("with the clash gone: got %q, want %q", got, "FH")
	}
}

// The two ways of missing out are different and must look different: a letter
// a sibling took is not yours to advertise (no underline), while a letter the
// context claimed still is (muted underline).
func TestAssignDistinguishesSiblingFromContext(t *testing.T) {
	// "Help" wants h; "History" wants h too and has no backup.
	got := letters(assignAccelerators(candsOf("&Help", "&History"), nil))
	if got != "H-" {
		t.Errorf("sibling-consumed: got %q, want %q", got, "H-")
	}
	// Now the context owns h, so neither can use it — but the first still
	// displays it, muted, and the second is still not entitled to it.
	clash := func(r rune) bool { return r == 'h' }
	if got := letters(assignAccelerators(candsOf("&Help", "&History"), clash)); got != "h-" {
		t.Errorf("context-clashed: got %q, want %q", got, "h-")
	}
}

// A muted letter is still claimed, so a later sibling cannot take it out from
// under the item displaying it. Without that the display would reshuffle every
// time an unrelated chord was claimed or released.
func TestAssignMutedLetterStaysClaimed(t *testing.T) {
	clash := func(r rune) bool { return r == 'h' }
	// Help mutes on h; History must not then be handed h.
	got := letters(assignAccelerators(candsOf("&Help", "&Histor&y"), clash))
	if got != "hY" {
		t.Errorf("got %q, want %q", got, "hY")
	}
}

// A mixed miss: the first candidate belongs to a sibling and the second is
// claimed by the context, so the muted underline goes on the second — the one
// that would have worked.
func TestAssignMutesTheLetterThatWouldHaveWorked(t *testing.T) {
	clash := func(r rune) bool { return r == 'b' }
	got := letters(assignAccelerators(candsOf("&A", "&A&B"), clash))
	if got != "Ab" {
		t.Errorf("got %q, want %q", got, "Ab")
	}
}

// A title with no markup takes part in nothing and shows nothing.
func TestAssignSkipsUnmarkedTitles(t *testing.T) {
	got := letters(assignAccelerators(candsOf("&File", "Untitled", "&Edit"), nil))
	if got != "F-E" {
		t.Errorf("got %q, want %q", got, "F-E")
	}
}

// Positions come back with the letter, so the painter knows where to underline
// even when a backup moved it.
func TestAssignReportsPosition(t *testing.T) {
	clash := func(r rune) bool { return r == 'h' }
	as := assignAccelerators(candsOf("&Hel&p"), clash)
	if as[0].Char != 'p' || as[0].Pos != 3 || !as[0].Active {
		t.Errorf("got %+v, want p at 3, active", as[0])
	}
	none := assignAccelerators(candsOf("&A", "&A"), nil)
	if none[1].Char != 0 || none[1].Pos != -1 {
		t.Errorf("no-underline case = %+v, want zero char and -1 pos", none[1])
	}
}

// The pattern is a whole key sequence with "*" standing in for the letter, and
// the letter's slot is wherever the author put it — not a prefix.
func TestFormAcceleratorKey(t *testing.T) {
	for _, c := range []struct {
		pattern string
		ch      rune
		want    string
	}{
		{"M-*", 'h', "M-h"},
		{"m-*", 'h', "m-h"},
		{"^X * Return", 'h', "^X h Return"},
		{"^X ^M 2 2 7 *", 'h', "^X ^M 2 2 7 h"},
		// No "*" forms nothing, which turns chord accelerators off without
		// disturbing the bare letters a focused bar answers to.
		{"M-F4", 'h', ""},
		{"", 'h', ""},
		{"M-*", 0, ""},
	} {
		if got := formAcceleratorKey(c.pattern, c.ch); got != c.want {
			t.Errorf("formAcceleratorKey(%q, %q) = %q, want %q", c.pattern, c.ch, got, c.want)
		}
	}
}

// End to end on a real bar: with nothing claiming the chords every menu is
// lit, and the live accelerators are published into the context so the chord
// resolves to the internal accelerator command.
func TestMenuBarPublishesLiveAccelerators(t *testing.T) {
	reg := core.NewKeyRegistryFromMap("default", map[string][]string{"Tab": {"focus_next"}})
	ctx := reg.BuildContext([]string{"focus_next"})

	bar := NewMenuBar()
	bar.SetAcceleratorChord("M-*")
	bar.SetKeyContext(ctx)
	for _, title := range []string{"&File", "&Edit", "&Help"} {
		bar.AddMenu(NewMenu(title))
	}

	for _, m := range bar.menus {
		if !bar.ShouldShowAccelerator(m) {
			t.Errorf("%q should be lit with nothing claiming its chord", m.title)
		}
	}
	for _, key := range []string{"M-f", "M-e", "M-h"} {
		ctx.Abandon()
		if got := ctx.Resolve(key); got != core.CommandAppAccelerator {
			t.Errorf("%s -> %q, want %q", key, got, core.CommandAppAccelerator)
		}
	}
}

// A chord the context already claims mutes that menu: the letter keeps its
// underline but loses the accelerator colour, and it is NOT published — so it
// cannot fire, which is what stops an accelerator beating the binding it is
// supposed to be yielding to.
func TestMenuBarMutesClaimedChord(t *testing.T) {
	reg := core.NewKeyRegistryFromMap("default", map[string][]string{"M-h": {"history_panel"}})
	ctx := reg.BuildContext([]string{"history_panel"})

	bar := NewMenuBar()
	bar.SetAcceleratorChord("M-*")
	bar.SetKeyContext(ctx)
	file, help := NewMenu("&File"), NewMenu("&Help")
	bar.AddMenu(file)
	bar.AddMenu(help)

	if !bar.ShouldShowAccelerator(file) {
		t.Error("File is unclaimed and should be lit")
	}
	if bar.ShouldShowAccelerator(help) {
		t.Error("Help's chord is claimed; it should not be lit")
	}
	if !bar.ShouldUnderlineAccelerator(help) {
		t.Error("Help's letter is still Help's and should keep its underline")
	}
	if got := ctx.Resolve("M-h"); got != "history_panel" {
		t.Errorf("M-h -> %q, want history_panel: the accelerator must not take it", got)
	}
}

// A backup letter keeps a menu reachable when its first choice is claimed.
func TestMenuBarUsesBackupLetter(t *testing.T) {
	reg := core.NewKeyRegistryFromMap("default", map[string][]string{"M-h": {"history_panel"}})
	ctx := reg.BuildContext([]string{"history_panel"})

	bar := NewMenuBar()
	bar.SetAcceleratorChord("M-*")
	bar.SetKeyContext(ctx)
	help := NewMenu("&Hel&p")
	bar.AddMenu(help)

	if !bar.ShouldShowAccelerator(help) {
		t.Error("Help should be lit on its backup letter")
	}
	if help.acceleratorChar != 'p' {
		t.Errorf("Help chose %q, want p", help.acceleratorChar)
	}
	ctx.Abandon()
	if got := ctx.Resolve("M-p"); got != core.CommandAppAccelerator {
		t.Errorf("M-p -> %q, want the accelerator command", got)
	}
}

// A letter an earlier sibling took is not this menu's to advertise: no colour
// and no underline, since the sibling is showing it lit on the same bar.
func TestMenuBarSiblingConsumedShowsNothing(t *testing.T) {
	bar := NewMenuBar()
	bar.SetAcceleratorChord("M-*")
	help, history := NewMenu("&Help"), NewMenu("&History")
	bar.AddMenu(help)
	bar.AddMenu(history)

	if !bar.ShouldShowAccelerator(help) {
		t.Error("the first Help should be lit")
	}
	if bar.ShouldUnderlineAccelerator(history) {
		t.Error("History's letter belongs to Help; it should not be underlined at all")
	}
	if history.acceleratorChar != 0 {
		t.Errorf("History kept %q; with no letter of its own it should have none", history.acceleratorChar)
	}
}

// A tree view separates two pairs the vocabulary could easily have merged.
//
// Enter begins the row edit; Space activates, which for a tree means expanding
// or collapsing the branch — Space deliberately refuses to begin a text edit.
// And the arrows are the generic movement, which a tree implements as
// collapse-or-walk-up, while Minus and Plus collapse and expand WITHOUT
// walking. One command each, four distinct behaviours.
func TestTreeViewSeparatesEditFromActivateAndCollapseFromMovement(t *testing.T) {
	tv := NewTreeView()
	for key, want := range map[string]string{
		"Return": core.CmdTrinketEdit,
		"Space":  core.CmdTrinketActivate,
		"Left":   core.CmdTrinketItemLeft,
		"Right":  core.CmdTrinketItemRight,
		"Minus":  core.CmdTrinketCollapse,
		"Plus":   core.CmdTrinketExpand,
	} {
		tv.AbandonKeySequence()
		if got := tv.KeyCommand(key); got != want {
			t.Errorf("%s -> %q, want %q", key, got, want)
		}
	}
}

// A button offers no edit, so Enter falls through to activate there — the same
// key, a different meaning, decided by what the trinket can do.
func TestEnterEditsOrActivatesByTrinket(t *testing.T) {
	b := NewButton("ok")
	if got := b.KeyCommand("Return"); got != core.CmdTrinketActivate {
		t.Errorf("button Enter -> %q, want %q", got, core.CmdTrinketActivate)
	}
	tv := NewTreeView()
	if got := tv.KeyCommand("Return"); got != core.CmdTrinketEdit {
		t.Errorf("tree Enter -> %q, want %q", got, core.CmdTrinketEdit)
	}
}

// A left that never collapses. Unbound by default — the arrows are the generic
// movement and a tree spends them on collapse-or-walk-up — so these exist to
// be mapped by a keymap that would rather separate the two.
func TestTreeViewColumnMovementNeverCollapses(t *testing.T) {
	core.DefaultKeyRegistry().Bind("C-Left", core.CmdTrinketColumnLeft)
	core.DefaultKeyRegistry().Bind("C-Right", core.CmdTrinketColumnRight)
	defer func() {
		core.DefaultKeyRegistry().Bind("C-Left", core.CmdWindowMoveLeft)
		core.DefaultKeyRegistry().Bind("C-Right", core.CmdWindowMoveRight)
	}()

	tv := NewTreeView()
	if got := tv.KeyCommand("C-Left"); got != core.CmdTrinketColumnLeft {
		t.Errorf("C-Left -> %q, want %q", got, core.CmdTrinketColumnLeft)
	}
	tv.AbandonKeySequence()
	if got := tv.KeyCommand("C-Right"); got != core.CmdTrinketColumnRight {
		t.Errorf("C-Right -> %q, want %q", got, core.CmdTrinketColumnRight)
	}

	// With fewer than two editable columns there is nowhere to go, and the
	// key is declined rather than silently swallowed.
	if tv.moveEnterTargetColumn(1) {
		t.Error("a tree with no editable columns reported a column move")
	}
}

// Sorting and the column chooser were reachable only by walking the header
// focus zones. These make each step mappable on its own.
func TestTreeViewSortCommands(t *testing.T) {
	tv := NewTreeView()
	tv.AddColumn(&TreeColumn{ID: "a", Caption: "A", Sortable: true})

	// The plain forms set a direction outright.
	tv.SortAscending()
	if sorted, _, desc := tv.Sorted(); !sorted || desc {
		t.Errorf("after SortAscending: sorted=%v descending=%v", sorted, desc)
	}
	tv.SortDescending()
	if sorted, _, desc := tv.Sorted(); !sorted || !desc {
		t.Errorf("after SortDescending: sorted=%v descending=%v", sorted, desc)
	}
	tv.SortOff()
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Error("SortOff left the view sorted")
	}

	// The toggle forms apply, then clear when already in that direction, so
	// one key does both.
	tv.ToggleSortAscending()
	if sorted, _, desc := tv.Sorted(); !sorted || desc {
		t.Error("first ToggleSortAscending should sort ascending")
	}
	tv.ToggleSortAscending()
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Error("second ToggleSortAscending should turn sorting off")
	}

	// The mode forms walk the cycle a header activation walks.
	tv.SortModeNext() // off -> ascending
	if sorted, _, desc := tv.Sorted(); !sorted || desc {
		t.Error("SortModeNext from off should sort ascending")
	}
	tv.SortModeNext() // ascending -> descending
	if _, _, desc := tv.Sorted(); !desc {
		t.Error("SortModeNext from ascending should reverse")
	}
	tv.SortModeNext() // descending -> off
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Error("SortModeNext from descending should turn sorting off")
	}
	// ...and prior walks it the other way.
	tv.SortModePrior()
	if sorted, _, desc := tv.Sorted(); !sorted || !desc {
		t.Error("SortModePrior from off should sort descending")
	}
}

// Every one of them is unbound by default: they exist to be mapped, and
// mapping one is what makes it reachable.
func TestSortCommandsAreUnboundByDefault(t *testing.T) {
	r := core.DefaultKeyRegistry()
	for _, cmd := range []string{
		core.CmdTrinketSortAscending, core.CmdTrinketSortDescending,
		core.CmdTrinketSortOff, core.CmdTrinketSortModeNext,
		core.CmdTrinketSortModePrior, core.CmdTrinketChooser,
		core.CmdTrinketExpandedToggle,
		core.CmdTrinketColumnLeft, core.CmdTrinketColumnRight,
	} {
		if keys := r.KeysFor(cmd); len(keys) != 0 {
			t.Errorf("%s is bound to %v by default; it should be mappable only", cmd, keys)
		}
	}
}
