package core

import "testing"

// A cell target stamps the runes it is handed, one cell at a time, left to
// right. So a run in a script that reads the other way has to arrive already
// turned over, with the Arabic exchanged for the forms that stand in for
// joining -- nothing downstream will do either.
func TestACellRunReadsTheWayItIsDrawn(t *testing.T) {
	t.Cleanup(func() { SetTextMeasurer(nil) })
	SetTextMeasurer(nil)

	const shalom = "שלום" // shin lamed vav mem-final

	// Turned over: the last letter of the word is the first cell drawn.
	got := CellRun(shalom, DirLTR)
	want := string([]rune{'ם', 'ו', 'ל', 'ש'})
	if got != want {
		t.Errorf("in an LTR line the word draws as %q, want %q -- an RTL run reorders "+
			"whichever way the line around it reads", got, want)
	}
	if CellRun(shalom, DirRTL) != want {
		t.Errorf("in an RTL line the word draws as %q, want the same", CellRun(shalom, DirRTL))
	}

	// English keeps its order, and asks for no work at all.
	if got := CellRun("hello", DirLTR); got != "hello" {
		t.Errorf("an English run draws as %q, want it untouched", got)
	}

	// A caller with no direction resolved gets its text back.
	if got := CellRun(shalom, DirInherit); got != shalom {
		t.Errorf("with no direction the run draws as %q, want it untouched", got)
	}

	// A word and its points: the point still follows the letter it composes
	// onto, so the terminal lands it on that cell and not the one beside it.
	pointed := string([]rune{'ש', 'ָ', 'ל'}) // shin + qamats, then lamed
	if got, want := CellRun(pointed, DirRTL), string([]rune{'ל', 'ש', 'ָ'}); got != want {
		t.Errorf("the pointed word draws as %q, want the point to travel with its letter (%q)", got, want)
	}

	// Arabic arrives joined: the letters become presentation forms, and
	// lam-alef becomes the one glyph that is the pair.
	lamAlef := CellRun("لا", DirRTL)
	if r := []rune(lamAlef); len(r) != 1 || r[0] != 0xFEFB {
		t.Errorf("lam-alef draws as %04X, want the single ligature FEFB", r)
	}

	// A bracket inside an RTL run faces the way that run reads.
	if got := CellRun("(ש)", DirRTL); got != "(ש)" {
		t.Errorf("the bracketed word draws as %q; each bracket should have become its "+
			"partner, so the pair still opens on the reading side", got)
	}
}

// On a pixel target the text engine orders and shapes whole paragraphs, so a
// run prepared here as well would be prepared twice.
func TestAPixelTargetPreparesItsOwnRuns(t *testing.T) {
	t.Cleanup(func() { SetTextMeasurer(nil) })
	SetTextMeasurer(stubMeasurer{})

	const shalom = "שלום"
	if got := CellRun(shalom, DirRTL); got != shalom {
		t.Errorf("with a text measurer installed the run comes back as %q, want it untouched", got)
	}
}

type stubMeasurer struct{}

func (stubMeasurer) MeasureText(f *Font, text string) Unit { return Unit(len([]rune(text))) * 8 }
