// bidifill measures what a terminal that runs its OWN bidi does with a
// background fill on a line carrying combining marks.
//
// macOS Terminal.app places such a fill wrongly: a selection bar over pointed
// Hebrew drifts and half-vanishes. What is not known -- and what this is for --
// is the LAW. Read off the screen, it says whether the displacement is a
// uniform shift, one that grows per mark, or a span of the wrong length; and
// whether the FOREGROUND is displaced with it or rides the glyph through
// intact. That decides whether the fault can be compensated for by sending the
// attributes in the order the terminal will consume them, rather than giving up
// the fill on such lines altogether.
//
// Run it in the terminal in question and read the two blocks:
//
//	go run ./examples/bidifill
//
// Every specimen is SIX cells, and every one of them is laid out so that the
// six cells, left to right on the screen, are coloured
//
//	red  green  yellow  blue  magenta  cyan
//
// in that order. That is the whole test: the letters differ from row to row,
// the colours never do. A row whose colours come out in that order is a row the
// terminal placed correctly, whatever its letters are; anywhere they come out
// in another order, or run out before the sixth cell, is the fault.
//
// The FILL block carries the sequence in the background, the INK block in the
// foreground. If the ink block reads correctly and the fill block does not,
// the terminal is keeping attributes with the glyphs and painting the
// background at a position it counted separately -- which is the case the
// compensation would be built on.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/hostterm"
	"github.com/phroun/kittytk/style"
)

// The sequence every specimen wears, left to right across its six cells.
var sequence = []struct {
	name  string
	color style.Color
}{
	{"red", style.ColorRed},
	{"green", style.ColorGreen},
	{"yellow", style.ColorYellow},
	{"blue", style.ColorBlue},
	{"magenta", style.ColorMagenta},
	{"cyan", style.ColorCyan},
}

// A specimen is six CELLS, given in the order they sit on the screen. Each
// entry is one cell's content: a base and whatever marks ride it, exactly as
// this toolkit's own backbuffer would hold them after ordering the line.
type specimen struct {
	label string
	cells []string
}

// Hebrew letters, and two marks that behave differently under folding: the
// qamats is a VOWEL, which has no presentation form and survives, and the
// dagesh is a POINT, which folds into its letter.
const (
	qamats = "ָ"
	dagesh = "ּ"
	acute  = "́"
)

// The specimens, each already turned over where its content reads
// right-to-left -- the leftmost cell first, which is how a line reaches the
// terminal.
var specimens = []specimen{
	// The control. Nothing turns over, nothing has a mark: any terminal places
	// this correctly, so a row that fails HERE means something else is wrong.
	{"ascii, no marks", []string{"a", "b", "c", "d", "e", "f"}},

	// Right-to-left, but every cell one plain letter: this separates the
	// REORDERING from the marks. Failing here and not above means the fault is
	// in the turn; failing only below means it is the marks.
	{"hebrew, no marks", []string{"ו", "ה", "ד", "ג", "ב", "א"}},

	// One mark on every letter: six cells, twelve codepoints. If the terminal
	// counts codepoints where the screen counts cells, this is where it starts.
	{"hebrew, 1 mark each", []string{
		"ו" + qamats, "ה" + qamats, "ד" + qamats,
		"ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Marks on the LEFT half only. A displacement that grows per mark shows
	// here as colours correct on the right and wrong on the left, or the
	// reverse -- which says which end it accumulates from.
	{"hebrew, marks left half", []string{
		"ו" + qamats, "ה" + qamats, "ד" + qamats, "ג", "ב", "א"}},

	// And on the RIGHT half only, the same question from the other side.
	{"hebrew, marks right half", []string{
		"ו", "ה", "ד", "ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Cluster sizes that differ across the row: none, one, two. A uniform shift
	// and a per-mark shift disagree about this row and agree about the others.
	{"hebrew, 0/1/2 marks", []string{
		"ו", "ה", "ד" + qamats, "ג" + qamats,
		"ב" + qamats + dagesh, "א" + qamats + dagesh}},

	// A point that FOLDS into its letter rather than a vowel that does not. If
	// this row is placed correctly and the vowel rows are not, folding is
	// enough on its own and the compensation need only cover what survives it.
	{"hebrew, folding point", []string{
		"ו" + dagesh, "ה" + dagesh, "ד" + dagesh,
		"ג" + dagesh, "ב" + dagesh, "א" + dagesh}},

	// Both directions in one row: two English letters, then four Hebrew ones.
	// The Hebrew is turned over within itself; the English is not.
	{"mixed, marked hebrew", []string{
		"a", "b", "ד" + qamats, "ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Marks with nothing right-to-left about them. If this row is displaced
	// too, the fault is about MARKS and not about the turn at all.
	{"ascii, combining marks", []string{
		"e" + acute, "e" + acute, "e" + acute,
		"e" + acute, "e" + acute, "e" + acute}},
}

const (
	labelCol    = 2
	anchorCol   = 28
	specimenCol = 29
)

// The six cells sit between two strong left-to-right letters, so a terminal
// running its own bidi has no neutral to resolve at either end and the block
// lands where it was put. That separates the two questions: whether the whole
// specimen moved, and whether the colours inside it came out in order.
const (
	leftAnchor  = "L"
	rightAnchor = "R"
)

func main() {
	opts := tui.DefaultTUIOptions()
	opts.EnableMouse = false
	b := tui.NewTUIBackend(opts)
	if err := b.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "bidifill:", err)
		os.Exit(1)
	}
	defer b.Shutdown()

	b.BeginFrame()
	draw(b)
	b.EndFrame()

	// Anything at all closes it.
	for {
		switch b.WaitEvent().(type) {
		case core.KeyPressEvent, core.QuitEvent:
			return
		}
	}
}

func draw(b *tui.TUIBackend) {
	m := b.Metrics()
	at := func(col, row int) (core.Unit, core.Unit) {
		return m.CellToUnitsX(col), m.CellToUnitsY(row)
	}
	plain := style.DefaultStyle().WithFg(style.ColorWhite)
	dim := style.DefaultStyle().WithFg(style.ColorBrightBlack)

	row := 0
	say := func(text string, s style.CellStyle) {
		x, y := at(labelCol, row)
		b.DrawText(x, y, text, s, nil)
		row++
	}

	// The whole thing is twenty-four rows, so it fits the shortest window
	// anyone is likely to run it in.
	applies, wordwise := core.HostAppliesBidi()
	say(fmt.Sprintf("bidifill -- %q reorders=%v wordwise=%v fill=%v",
		hostterm.Detect(), applies, wordwise, core.HostMiscountsFill()), plain)
	say("the six cells between L and R are red green yellow blue magenta cyan,", dim)
	say("left to right. any other order is the fault. press a key to quit.", dim)
	row++

	// Fill block: the sequence in the background.
	say("FILL -- the sequence is the background", plain)
	for _, sp := range specimens {
		drawSpecimen(b, at, row, sp, func(c style.Color) style.CellStyle {
			return style.DefaultStyle().WithFg(style.ColorBlack).WithBg(c)
		})
		row++
	}

	// Ink block: the sequence in the foreground, on one ground throughout.
	say("INK -- the sequence is the foreground", plain)
	for _, sp := range specimens {
		drawSpecimen(b, at, row, sp, func(c style.Color) style.CellStyle {
			return style.DefaultStyle().WithFg(c).WithBg(style.ColorBlack)
		})
		row++
	}
}

// drawSpecimen lays one row down cell by cell, so each cell's content and its
// colour are placed together and neither can be blamed on the other.
func drawSpecimen(b *tui.TUIBackend, at func(col, row int) (core.Unit, core.Unit),
	row int, sp specimen, wear func(style.Color) style.CellStyle) {
	plain := style.DefaultStyle().WithFg(style.ColorWhite)
	x, y := at(labelCol, row)
	b.DrawText(x, y, sp.label, plain, nil)

	x, y = at(anchorCol, row)
	b.DrawText(x, y, leftAnchor, plain, nil)
	for i, cell := range sp.cells {
		if i >= len(sequence) {
			break
		}
		x, y := at(specimenCol+i, row)
		b.DrawText(x, y, cell, wear(sequence[i].color), nil)
	}
	x, y = at(specimenCol+len(sequence), row)
	b.DrawText(x, y, rightAnchor, plain, nil)
}
