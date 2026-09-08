// bidifill measures what a terminal that runs its OWN bidi does with a
// background fill on a line carrying combining marks.
//
// macOS Terminal.app places such a fill wrongly: a selection bar over pointed
// Hebrew drifts and half-vanishes. Whether that can be compensated for, and
// how, is what this is for. Run it in the terminal in question:
//
//	go run ./examples/bidifill
//
// A key moves between two pages, and a second closes it.
//
// The FIRST page names the fault. Every specimen is SIX cells, and every one of
// them is laid out so that the six cells, left to right on the screen, are
// coloured
//
//	red  green  yellow  blue  magenta  cyan
//
// in that order. That is the whole test: the letters differ from row to row,
// the colours never do. A row whose colours come out in that order is a row the
// terminal placed correctly, whatever its letters are; anywhere they come out
// in another order, or run out before the sixth cell, is the fault. Each block
// carries a ruler over the columns a colour can land in, the two anchors
// included, so a wrong row can be read off column by column.
//
// The FILL block carries the sequence in the background and the INK block in
// the foreground, which separates displacement from loss: ink rides a glyph
// through a reorder, background does not. The row wearing ONE colour across all
// six cells separates them again, and more sharply -- a displaced fill still
// paints the whole block, and only a fill that ran out leaves part of it bare.
//
// The SECOND page asks what to do about a terminal that runs out. It sends the
// same six pointed letters six ways: as the backend sends them now, with an
// attribute for every codepoint rather than every cell, and with spare
// attributes carried in on zero-width characters -- after the row, before it,
// and one per mark. Whichever row comes back in the right order names the fix,
// and a bright colour anywhere is a spare the terminal took.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/phroun/khatool"
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
	// solid wears one colour across all six cells instead of the sequence. It
	// asks a different question: whether a fill that comes up short has been
	// displaced off the end of the block, or has genuinely run out.
	solid bool
	// control marks the ones the INK block repeats. Ink rides a glyph through
	// the reorder, so the point of repeating it is to show that it still does
	// in whatever terminal this is being run in -- a few rows prove that as
	// well as all of them, and the rows saved go to the fill.
	control bool
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
	// Nothing turns over, nothing has a mark: any terminal places this
	// correctly, so a row that fails HERE means something else is wrong.
	{label: "ascii, no marks", control: true,
		cells: []string{"a", "b", "c", "d", "e", "f"}},

	// Right-to-left, but every cell one plain letter: this separates the
	// REORDERING from the marks. Failing here and not above means the fault is
	// in the turn; failing only below means it is the marks.
	{label: "hebrew, no marks",
		cells: []string{"ו", "ה", "ד", "ג", "ב", "א"}},

	// ONE mark, at the left end of the block, and nothing else on the row. This
	// is the smallest step away from the row above, so whatever moves between
	// the two is what one mark costs.
	{label: "hebrew, mark on cell 0",
		cells: []string{"ו" + qamats, "ה", "ד", "ג", "ב", "א"}},

	// The same single mark at the RIGHT end. Together with the row above it
	// says whether the displacement reaches back towards the start of the row
	// or only forwards from the mark.
	{label: "hebrew, mark on cell 5",
		cells: []string{"ו", "ה", "ד", "ג", "ב", "א" + qamats}},

	// One mark on every letter: six cells, twelve codepoints. If the terminal
	// counts codepoints where the screen counts cells, this is where it starts.
	{label: "hebrew, 1 mark each", control: true,
		cells: []string{
			"ו" + qamats, "ה" + qamats, "ד" + qamats,
			"ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Every cell of that same row in ONE colour. A fill that comes up short
	// against the sequence has either been pushed off the end of the block or
	// has run out partway; those look alike under six colours and quite
	// different under one, where a displacement still paints the whole block.
	{label: "hebrew, all six red", solid: true,
		cells: []string{
			"ו" + qamats, "ה" + qamats, "ד" + qamats,
			"ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Marks on the LEFT half only. A displacement that grows per mark shows
	// here as colours correct on the right and wrong on the left, or the
	// reverse -- which says which end it accumulates from.
	{label: "hebrew, marks left half",
		cells: []string{
			"ו" + qamats, "ה" + qamats, "ד" + qamats, "ג", "ב", "א"}},

	// And on the RIGHT half only, the same question from the other side.
	{label: "hebrew, marks right half",
		cells: []string{
			"ו", "ה", "ד", "ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Cluster sizes that differ across the row: none, one, two. A uniform shift
	// and a per-mark shift disagree about this row and agree about the others.
	{label: "hebrew, 0/1/2 marks", control: true,
		cells: []string{
			"ו", "ה", "ד" + qamats, "ג" + qamats,
			"ב" + qamats + dagesh, "א" + qamats + dagesh}},

	// A point that FOLDS into its letter rather than a vowel that does not. If
	// this row is placed correctly and the vowel rows are not, folding is
	// enough on its own and the compensation need only cover what survives it.
	{label: "hebrew, folding point", control: true,
		cells: []string{
			"ו" + dagesh, "ה" + dagesh, "ד" + dagesh,
			"ג" + dagesh, "ב" + dagesh, "א" + dagesh}},

	// Both directions in one row: two English letters, then four Hebrew ones.
	// The Hebrew is turned over within itself; the English is not.
	{label: "mixed, marked hebrew",
		cells: []string{
			"a", "b", "ד" + qamats, "ג" + qamats, "ב" + qamats, "א" + qamats}},

	// Marks with nothing right-to-left about them. If this row is displaced
	// too, the fault is about MARKS and not about the turn at all.
	{label: "ascii, combining marks",
		cells: []string{
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

// A name for every column a colour can land in, the anchors included -- a fill
// displaced off the block lands on one of those, and it needs a name too. It
// sits in each block's heading row, so naming the columns costs no rows.
const ruler = "<012345>"

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
	if !waitKey(b) {
		return
	}

	probes()
	waitKey(b)
}

// waitKey blocks until someone presses something, and reports whether to carry
// on -- a quit says not to.
func waitKey(b *tui.TUIBackend) bool {
	for {
		switch b.WaitEvent().(type) {
		case core.KeyPressEvent:
			return true
		case core.QuitEvent:
			return false
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
	say("cells 0..5 are red green yellow blue magenta cyan, left to right. read a", dim)
	say("wrong row off the ruler, colour by colour. press a key to quit.", dim)
	row++

	// Each block's heading carries the ruler, so every column a colour can land
	// in has a name without spending a row on it.
	heading := func(text string) {
		x, y := at(labelCol, row)
		b.DrawText(x, y, text, plain, nil)
		x, y = at(anchorCol, row)
		b.DrawText(x, y, ruler, dim, nil)
		row++
	}

	// Fill block: the sequence in the background.
	heading("FILL (background)")
	for _, sp := range specimens {
		drawSpecimen(b, at, row, sp, func(c style.Color) style.CellStyle {
			return style.DefaultStyle().WithFg(style.ColorBlack).WithBg(c)
		})
		row++
	}

	// Ink block: the sequence in the foreground, on one ground throughout.
	heading("INK (foreground)")
	for _, sp := range specimens {
		if !sp.control {
			continue
		}
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
		colour := sequence[i].color
		if sp.solid {
			colour = sequence[0].color
		}
		x, y := at(specimenCol+i, row)
		b.DrawText(x, y, cell, wear(colour), nil)
	}
	x, y = at(specimenCol+len(sequence), row)
	b.DrawText(x, y, rightAnchor, plain, nil)
}

// A zero-width character carries an attribute without claiming a cell -- which
// is the whole idea the probe page is built on.
const zwj = "‍"

// The probe page asks one question of the row that fails worst: a terminal that
// runs out of background partway along a line has been given six attributes for
// twelve codepoints, so what happens if it is given twelve?
//
// Six ways of saying the same colours go out, and the row that comes back in
// the right order names the fix. A BRIGHT colour anywhere is a padding
// attribute the terminal took, which says the padding was consumed even where
// it did not help.
//
// This is written straight to the terminal rather than through the backbuffer.
// A cell holds one style, and every row here deliberately puts a style on a
// codepoint that shares its cell with another -- there is no cell grid that can
// say it. Nothing repaints afterwards, so the rows stand until a key is
// pressed.
var probeCells = []string{
	"ו" + qamats, "ה" + qamats, "ד" + qamats,
	"ג" + qamats, "ב" + qamats, "א" + qamats,
}

// paint is the attributes for one of the six sequence colours: black ink on
// that ground, bright where a padding attribute wants telling apart from a real
// one.
func paint(i int, bright bool) string {
	ground := 41 + i
	if bright {
		ground = 101 + i
	}
	return fmt.Sprintf("\033[0m\033[30m\033[%dm", ground)
}

// The six ways of sending the row. Each takes the emission order and the colour
// its slot wears, and returns the bytes between the anchors.
var probeWays = []struct {
	label string
	emit  func(order []int, colour func(k int) string) string
}{
	// What the backend sends today: one attribute per CELL, ahead of the base,
	// with the marks riding along under it.
	{"as we send it now", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			b.WriteString(colour(k))
			b.WriteString(probeCells[i])
		}
		return b.String()
	}},

	// One attribute per CODEPOINT, the mark given its own copy of the colour its
	// base wears. No extra characters, nothing moved -- if this is enough, the
	// fix costs nothing at all.
	{"an SGR per codepoint", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			for _, r := range probeCells[i] {
				b.WriteString(colour(k))
				b.WriteRune(r)
			}
		}
		return b.String()
	}},

	// One attribute per codepoint again, but carried in after the cluster on a
	// zero-width character rather than put on the mark. Same count, and the
	// cluster itself is left alone.
	{"a zwj per mark, same", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			b.WriteString(colour(k))
			b.WriteString(probeCells[i])
			for range []rune(probeCells[i])[1:] {
				b.WriteString(colour(k))
				b.WriteString(zwj)
			}
		}
		return b.String()
	}},

	// Six spare attributes at the END of the line, as the question was put: does
	// the terminal reach past the row for the ones it ran out of?
	{"6 bright zwj at end", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			b.WriteString(colour(k))
			b.WriteString(probeCells[i])
		}
		for k := range order {
			b.WriteString(paint(k, true))
			b.WriteString(zwj)
		}
		return b.String()
	}},

	// And six at the START, since what survives on the failing rows is the
	// LEFTMOST fill -- so the front is where the terminal appears to be counting
	// from.
	{"6 bright zwj at start", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k := range order {
			b.WriteString(paint(k, true))
			b.WriteString(zwj)
		}
		for k, i := range order {
			b.WriteString(colour(k))
			b.WriteString(probeCells[i])
		}
		return b.String()
	}},

	// Both together: an attribute for every codepoint AND spares beyond the end.
	{"per codepoint + 6 end", func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			for _, r := range probeCells[i] {
				b.WriteString(colour(k))
				b.WriteRune(r)
			}
		}
		for k := range order {
			b.WriteString(paint(k, true))
			b.WriteString(zwj)
		}
		return b.String()
	}},
}

func probes() {
	// The row goes out turned back, exactly as the backend turns it back, so
	// the host's own pass lands it the right way round.
	bases := make([]rune, len(probeCells))
	for i, c := range probeCells {
		bases[i] = []rune(c)[0]
	}
	order, styleOf, _ := khatool.FlipRuns(bases, false)

	var out strings.Builder
	out.WriteString("\033[2J")
	row := 1
	say := func(text string) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s", row, text)
		row++
	}
	// A row of the probe: the label, the anchors, and one way of sending it.
	lay := func(label, body string) {
		pad := strings.Repeat(" ", anchorCol-labelCol-len(label))
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s%s%s%s\033[0m\033[37m%s",
			row, label, pad, leftAnchor, body, rightAnchor)
		row++
	}
	block := func(heading string, solid bool) {
		pad := strings.Repeat(" ", anchorCol-labelCol-len(heading))
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s%s\033[90m%s",
			row, heading, pad, ruler)
		row++
		for _, way := range probeWays {
			lay(way.label, way.emit(order, func(k int) string {
				if solid {
					return paint(0, false)
				}
				return paint(styleOf[k], false)
			}))
		}
	}

	say("PROBE -- every row is the same six pointed letters, sent six ways.")
	say("correct is red green yellow blue magenta cyan across 0..5. a bright")
	say("colour is a padding attribute the terminal took.")
	row++
	block("SEQUENCE", false)
	row++
	block("ALL RED", true)
	row++
	say("press a key to quit.")

	os.Stdout.WriteString(out.String())
}
