// bidifill measures what a terminal that runs its OWN bidi does with a
// background fill on a line carrying combining marks.
//
// macOS Terminal.app places such a fill wrongly: a selection bar over pointed
// Hebrew drifts and half-vanishes. Whether that can be compensated for, and
// how, is what this is for. Run it in the terminal in question:
//
//	go run ./examples/bidifill
//
// A key moves between the five pages, and one more closes it.
//
// What it says about Apple Terminal, which is what it was written for:
//
//   - The fill for a right-to-left run slides LEFT one column per combining
//     mark that survives folding, the whole span together, and whatever slides
//     past the run's left edge is lost -- it bleeds one column onto the
//     neighbour and no further. A mark on the first cluster sent costs nothing,
//     so the drift is the marks consumed before the fill's origin is set.
//
//   - Foreground colour and weight ride each glyph through untouched.
//
//   - A point that folds into its base leaves the count, so a line written with
//     presentation forms is placed correctly and only surviving vowels drift.
//
//   - An attribute is consumed only by a character the terminal gives a cell
//     to. U+200D and U+200B are consumed and cancel the drift exactly, by
//     splitting each cluster so that its mark stands in a cell of its own --
//     which is the word coming apart. U+2060, U+FEFF and U+034F take no cell
//     and consume nothing at all. Carriers gathered AFTER a run cost no
//     columns and do nothing; carriers spread through one work and cost the
//     word its shape. Paying the columns back with a reposition writes over
//     the carriers and takes their fill with them, and an overrun with
//     autowrap off parks the cursor exactly as it is documented to, which
//     rescues nothing because the cost is not at the end of the line.
//
//     So there is nowhere to put a spare attribute that is both free and
//     effective, and a run whose marks survive folding has to wear what rides
//     the glyph instead.
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
// The SECOND page asks what a terminal that misplaces the fill will accept
// instead. It sends the same six pointed letters six ways: as the backend sends
// them now, with an attribute for every codepoint rather than every cell, and
// with spare attributes carried in on zero-width characters -- after the row,
// before it, and one per mark. Whichever row comes back in the right order
// names the fix, and a bright colour anywhere is a spare the terminal took.
//
// The THIRD page takes whichever shape moved the fill and tries it with every
// carrier that ought to claim no cell. Here the reading has two halves and both
// must hold: the colours in order, AND the R still standing under the ruler's
// closing mark. A carrier that fixed the colours by making the line longer has
// failed.
//
// The FOURTH page asks whether that length can be paid back. An emitter
// addresses every cell it writes, so it can spend the carriers' columns inside
// the run and then put the cursor on the column the next cell belongs in. The
// row is four pointed letters and then two plain ones, which keeps the two
// questions apart: whether the run's own fill lands, and whether what follows
// it is still where it was put.
//
// The FIFTH page measures what a carrier costs. Six of them moved the closing
// anchor by about two columns on the third page rather than by six, so the cost
// is not one cell apiece, and a count or a placing that costs nothing while
// still being consumed would be the fix. Each row is four cells, then some
// carriers, then a marker, and where the marker lands against the ruler is the
// answer. The page ends by running a row far past the right edge with autowrap
// off, to see whether the cursor parks as it is supposed to and leaves the rows
// around it alone.
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

	// A right-to-left run in the MIDDLE of a row, with cells of its own on both
	// sides, and no marks anywhere. This is the control for the row below it:
	// whatever these two rows do differently is what the marks did, which is
	// readable without having to judge a column by eye.
	{label: "clean in the middle",
		cells: []string{"a", "b", "ד", "ג", "c", "d"}},

	// The same row with the run pointed. If its neighbours c and d come out as
	// they do above, giving up that run is enough; if the marks pull the fill
	// off cells outside the run, it is not.
	{label: "marks in the middle",
		cells: []string{
			"a", "b", "ד" + qamats, "ג" + qamats, "c", "d"}},

	// Marks with nothing right-to-left about them. If this row is displaced
	// too, the fault is about MARKS and not about the turn at all.
	{label: "ascii, combining marks",
		cells: []string{
			"e" + acute, "e" + acute, "e" + acute,
			"e" + acute, "e" + acute, "e" + acute}},
}

const (
	labelCol    = 2
	anchorCol   = 27
	specimenCol = 29 // one blank column clear of the anchor
)

// The six cells sit between two strong left-to-right letters, so a terminal
// running its own bidi has no neutral to resolve at either end and the block
// lands where it was put. That separates the two questions: whether the whole
// specimen moved, and whether the colours inside it came out in order.
//
// A blank column stands between each anchor and the block. A fill that runs
// past the block's edge lands in that gap, where it reads as a colour with
// black on both sides -- and cannot be mistaken for the first or last cell,
// which is exactly the mistake a colour touching the anchor invites.
const (
	leftAnchor  = "L"
	rightAnchor = "R"
)

// A name for every column a colour can land in, the gaps and the anchors
// included -- a fill displaced off the block lands on one of those, and it
// needs a name too. It sits in each block's heading row, so naming the columns
// costs no rows.
const ruler = "< 012345 >"

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
	if !waitKey(b) {
		return
	}

	carriers()
	if !waitKey(b) {
		return
	}

	relocation()
	if !waitKey(b) {
		return
	}

	carrierCost()
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
	say("wrong row off the ruler, colour by colour. press a key for the probe.", dim)
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
	x, y = at(specimenCol+len(sequence)+1, row) // clear of the block
	b.DrawText(x, y, rightAnchor, plain, nil)
}

// The raw pages below are written straight to the terminal rather than through
// the backbuffer. A cell holds one style, and every row here deliberately puts
// a style on a codepoint that shares its cell with another -- there is no cell
// grid that can say it. Nothing repaints afterwards, so the rows stand until a
// key is pressed.

// The row that fails worst, and the one every raw page is built from.
var probeCells = []string{
	"ו" + qamats, "ה" + qamats, "ד" + qamats,
	"ג" + qamats, "ב" + qamats, "א" + qamats,
}

// Characters that claim no cell of their own, as candidates for carrying an
// attribute into a terminal that wants one per codepoint. Which of them a given
// terminal agrees is zero-width is the question; a row whose R has moved out
// from under the ruler's ">" was made longer by its carrier, and that carrier is
// no use.
const (
	zwj    = "\u200D" // zero width joiner
	zwsp   = "\u200B" // zero width space
	wj     = "\u2060" // word joiner
	zwnbsp = "\uFEFF" // zero width no-break space
	cgj    = "\u034F" // combining grapheme joiner
)

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

// A way of sending the row: a label, and the bytes that go between the anchors.
type way struct {
	label string
	emit  func(order []int, colour func(k int) string) string
}

// perMark sends one attribute per codepoint, the spare ones carried in after
// each cluster on a character that should claim no cell. This is the shape that
// moved the fill; which carrier leaves the line its own length is what decides
// whether it is usable.
func perMark(carrier string) func([]int, func(int) string) string {
	return func(order []int, colour func(int) string) string {
		var b strings.Builder
		for k, i := range order {
			b.WriteString(colour(k))
			b.WriteString(probeCells[i])
			for range []rune(probeCells[i])[1:] {
				b.WriteString(colour(k))
				b.WriteString(carrier)
			}
		}
		return b.String()
	}
}

// plainRun is what the backend sends today: one attribute per CELL, ahead of
// the base, with the marks riding along under it.
func plainRun(order []int, colour func(int) string) string {
	var b strings.Builder
	for k, i := range order {
		b.WriteString(colour(k))
		b.WriteString(probeCells[i])
	}
	return b.String()
}

// spares is six bright attributes on zero-width carriers, for the ends.
func spares(order []int, carrier string) string {
	var b strings.Builder
	for k := range order {
		b.WriteString(paint(k, true))
		b.WriteString(carrier)
	}
	return b.String()
}

// The first raw page: six ways of saying the same colours, to find out what a
// terminal that misplaces the fill will accept instead.
var probeWays = []way{
	{"as we send it now", plainRun},

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

	// The same count again, carried in on a zero-width character rather than put
	// on the mark, leaving the cluster alone.
	{"a zwj per mark, same", perMark(zwj)},

	// Six spare attributes at the END of the line: does the terminal reach past
	// the row for the ones it did not place?
	{"6 bright zwj at end", func(order []int, colour func(int) string) string {
		return plainRun(order, colour) + spares(order, zwj)
	}},

	// And six at the START, since what survives on the failing rows is the
	// LEFTMOST fill -- so the front is where the terminal appears to be counting
	// from.
	{"6 bright zwj at start", func(order []int, colour func(int) string) string {
		return spares(order, zwj) + plainRun(order, colour)
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
		b.WriteString(spares(order, zwj))
		return b.String()
	}},
}

// The second raw page: the shape that worked, tried with every carrier, to find
// one the terminal will take an attribute from without giving it a cell.
var carrierWays = []way{
	{"none (as we send it)", plainRun},
	{"zwj      U+200D", perMark(zwj)},
	{"zwsp     U+200B", perMark(zwsp)},
	{"word joiner U+2060", perMark(wj)},
	{"zwnbsp   U+FEFF", perMark(zwnbsp)},
	{"grapheme joiner 034F", perMark(cgj)},
}

// page writes one raw page: some lines of its own, then every way of sending
// the row twice -- once wearing the sequence, once all in one colour.
func page(intro []string, ways []way, footer string) {
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
	pad := func(text string) string {
		return text + strings.Repeat(" ", anchorCol-labelCol-len(text))
	}
	block := func(heading string, solid bool) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s\033[90m%s",
			row, pad(heading), ruler)
		row++
		for _, w := range ways {
			body := w.emit(order, func(k int) string {
				if solid {
					return paint(0, false)
				}
				return paint(styleOf[k], false)
			})
			fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s%s%s\033[0m\033[37m%s",
				row, pad(w.label), leftAnchor+" ", body, " "+rightAnchor)
			row++
		}
	}

	for _, line := range intro {
		say(line)
	}
	row++
	block("SEQUENCE", false)
	row++
	block("ALL RED", true)
	row++
	say(footer)

	os.Stdout.WriteString(out.String())
}

func probes() {
	page([]string{
		"PROBE -- every row is the same six pointed letters, sent six ways.",
		"correct is red green yellow blue magenta cyan across 0..5. a bright",
		"colour is a padding attribute the terminal took.",
	}, probeWays, "press a key for the carriers.")
}

func carriers() {
	page([]string{
		"CARRIERS -- one spare attribute per mark, on a character that should",
		"claim no cell. correct is red green yellow blue magenta cyan across",
		"0..5 AND an R still under the ruler's >. an R that moved is no use.",
	}, carrierWays, "press a key to quit.")
}

// The fourth page asks whether the carrier's cost can be paid back.
//
// A carrier that cancels the drift takes a cell to do it, so everything after
// the run is pushed along -- which page three reads as an R out of place. But
// the emitter addresses every cell it writes anyway, so it can put the cursor
// back: send the run with its carriers, then move to the column the next cell
// belongs in and carry on from there. The carriers' columns are spent inside
// the run, and nothing downstream need know.
//
// The row is four pointed letters and then two plain ones, so the two questions
// are separate and both visible: whether the run's own fill lands on 0..3, and
// whether c and d are still on 4 and 5 wearing magenta and cyan.
var relocCells = []string{
	"ד" + qamats, "ג" + qamats, "ב" + qamats, "א" + qamats, "c", "d",
}

// The column, counting from one, that a given cell of the block sits in.
func cellColumn(i int) int { return specimenCol + 1 + i }

var relocWays = []struct {
	label string
	emit  func(row int, colour func(k int) string) string
}{
	// The control: what the backend sends today.
	{"as we send it now", func(row int, colour func(int) string) string {
		return relocRun(colour, "", false) + relocTail(colour, 0)
	}},

	// Carriers inside the run and nothing done about the cost. The fill should
	// land and c and d should be pushed off their columns.
	{"carriers, no fixup", func(row int, colour func(int) string) string {
		return relocRun(colour, zwj, false) + relocTail(colour, 0)
	}},

	// The same, with the cursor put back before the tail is written.
	{"carriers, cursor back", func(row int, colour func(int) string) string {
		return relocRun(colour, zwj, false) + relocTail(colour, row)
	}},

	// And with the carriers gathered after the run rather than spread through
	// it, which is a smaller change to make in an emitter if it works.
	{"carriers after, back", func(row int, colour func(int) string) string {
		return relocRun(colour, zwj, true) + relocTail(colour, row)
	}},
}

// relocRun writes the right-to-left run: its four clusters in the order a
// reordering host wants them, each with its attributes. carrier, when given, is
// added once per mark -- after each cluster, or all together at the end when
// trailing is set.
func relocRun(colour func(k int) string, carrier string, trailing bool) string {
	var b strings.Builder
	spare := 0
	for k := 3; k >= 0; k-- { // the run turned back, as the backend turns it
		b.WriteString(colour(k))
		b.WriteString(relocCells[k])
		marks := len([]rune(relocCells[k])) - 1
		if carrier == "" {
			continue
		}
		if trailing {
			spare += marks
			continue
		}
		for i := 0; i < marks; i++ {
			b.WriteString(colour(k))
			b.WriteString(carrier)
		}
	}
	for i := 0; i < spare; i++ {
		b.WriteString(colour(0))
		b.WriteString(carrier)
	}
	return b.String()
}

// relocTail writes the two plain cells that follow the run. A non-zero row puts
// the cursor back on the column they belong in first, paying back whatever the
// carriers spent.
func relocTail(colour func(k int) string, row int) string {
	var b strings.Builder
	if row > 0 {
		fmt.Fprintf(&b, "\033[%d;%dH", row, cellColumn(4))
	}
	for k := 4; k < 6; k++ {
		b.WriteString(colour(k))
		b.WriteString(relocCells[k])
	}
	return b.String()
}

func relocation() {
	var out strings.Builder
	out.WriteString("\033[2J")
	row := 1
	say := func(text string) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s", row, text)
		row++
	}
	pad := func(text string) string {
		return text + strings.Repeat(" ", anchorCol-labelCol-len(text))
	}
	block := func(heading string, solid bool) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s\033[90m%s",
			row, pad(heading), ruler)
		row++
		for _, w := range relocWays {
			colour := func(k int) string {
				if solid {
					return paint(0, false)
				}
				return paint(k, false)
			}
			fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s%s%s\033[0m\033[37m%s",
				row, pad(w.label), leftAnchor+" ", w.emit(row, colour), " "+rightAnchor)
			row++
		}
	}

	say("RELOCATION -- four pointed letters on 0..3, then c and d on 4 and 5.")
	say("correct is red green yellow blue magenta cyan, c and d on their own")
	say("columns, and the R back under the ruler's closing mark.")
	row++
	block("SEQUENCE", false)
	row++
	block("ALL RED", true)
	row++
	say("press a key to quit.")

	os.Stdout.WriteString(out.String())
}

// The fifth page measures what a carrier COSTS, and whether an overrun is safe.
//
// Six carriers moved the closing anchor by about two columns on the third page,
// not by six, so a carrier plainly does not cost a whole cell. If there is a
// count or a placing where it costs NOTHING and is still consumed, that is the
// fix; and if the cost depends on what it follows, that says where to look.
//
// Each row is four cells, then some carriers, then a marker. Where the marker
// lands against the ruler is the cost, read off directly: on 4 it cost nothing,
// on 5 it cost one column, and so on.
const marker = "\033[0m\033[30m\033[107mX"

var costCells = struct{ ascii, hebrew, pointed []string }{
	ascii:   []string{"a", "b", "c", "d"},
	hebrew:  []string{"ד", "ג", "ב", "א"},
	pointed: []string{"ד" + qamats, "ג" + qamats, "ב" + qamats, "א" + qamats},
}

// costRow writes four cells and then n carriers -- spread through the cells
// when inside is set, gathered after them when it is not. Right-to-left cells
// go out turned back, as the backend turns them back.
func costRow(cells []string, rtl bool, n int, inside bool) string {
	var b strings.Builder
	at := func(k int) string { return paint(k, false) }
	each := 0
	if inside && len(cells) > 0 {
		each = n / len(cells)
	}
	order := []int{0, 1, 2, 3}
	if rtl {
		order = []int{3, 2, 1, 0}
	}
	spent := 0
	for _, k := range order {
		b.WriteString(at(k))
		b.WriteString(cells[k])
		for i := 0; i < each && spent < n; i++ {
			b.WriteString(at(k))
			b.WriteString(zwj)
			spent++
		}
	}
	for ; spent < n; spent++ {
		b.WriteString(at(3))
		b.WriteString(zwj)
	}
	return b.String() + marker
}

func carrierCost() {
	var out strings.Builder
	out.WriteString("\033[2J")
	row := 1
	say := func(text string) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s", row, text)
		row++
	}
	pad := func(text string) string {
		return text + strings.Repeat(" ", anchorCol-labelCol-len(text))
	}
	lay := func(label, body string) {
		fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s%s%s",
			row, pad(label), leftAnchor+" ", body)
		row++
	}

	say("COST -- four cells, then carriers, then a marker. where the marker")
	say("lands is what the carriers cost: on 4 they cost nothing at all, on 5")
	say("one column each two of them, on 8 a column apiece.")
	row++

	fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  %s\033[90m%s",
		row, pad("COST"), ruler)
	row++
	lay("ascii, no carriers", costRow(costCells.ascii, false, 0, false))
	lay("ascii, 4 after", costRow(costCells.ascii, false, 4, false))
	lay("hebrew, 4 after", costRow(costCells.hebrew, true, 4, false))
	lay("pointed, 4 after", costRow(costCells.pointed, true, 4, false))
	lay("pointed, 4 inside", costRow(costCells.pointed, true, 4, true))
	lay("pointed, 8 inside", costRow(costCells.pointed, true, 8, true))
	row++

	// And the overrun, with autowrap off: a row far longer than the screen,
	// then the row after it. If parking the cursor works, the second row is
	// where it was put and nothing has scrolled.
	fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  OVERRUN -- autowrap off, then a row far longer than the screen", row)
	row++
	fmt.Fprintf(&out, "\033[?7l\033[%d;1H\033[0m\033[37m  %s", row, strings.Repeat("=", 400))
	row++
	fmt.Fprintf(&out, "\033[%d;1H\033[0m\033[37m  THIS LINE IS WHERE IT WAS PUT -- and nothing above it moved\033[?7h", row)
	row += 2
	say("press a key to quit.")

	os.Stdout.WriteString(out.String())
}
