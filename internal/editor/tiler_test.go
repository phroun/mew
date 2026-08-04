package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// glyphCols returns, per 1-based screen row, the set of 1-based columns at which
// the rune `target` was painted. It walks the frame's cursor-position (CUP)
// segments the way rowCells does, but records only cells carrying `target` —
// so background spaces that fill the rest of the row are ignored.
func glyphCols(frame string, target rune) map[int][]int {
	cup := regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	out := map[int][]int{}
	idx := cup.FindAllStringSubmatchIndex(frame, -1)
	for i, m := range idx {
		row := 0
		for _, c := range frame[m[2]:m[3]] {
			row = row*10 + int(c-'0')
		}
		col := 0
		for _, c := range frame[m[4]:m[5]] {
			col = col*10 + int(c-'0')
		}
		end := len(frame)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		seg := sgr.ReplaceAllString(frame[m[1]:end], "")
		seg = strings.ReplaceAll(seg, "\x1b[K", "")
		n := col // 1-based column of the next glyph
		for _, r := range seg {
			if r == '\x1b' || r < 0x20 {
				continue
			}
			if r == target {
				out[row] = append(out[row], n)
			}
			n += textwidth.Rune(r)
		}
	}
	return out
}

// TestTilerFramesMainToHalfWidth is the minimal ifitfits geometry-integration
// check: the tiler splits its "main" tile against an empty "blank" tile to the
// right, so the painted main document is confined to the LEFT HALF of the
// screen. On an 80-column terminal the document's glyphs must all fall in the
// left ~40 columns, with nothing painted into the right half.
func TestTilerFramesMainToHalfWidth(t *testing.T) {
	// A line wider than the screen, so a full-width main would paint 'x' across
	// all 80 columns; a half-width main stops near column 40.
	e, _, out := newRenderedEditor(t, strings.Repeat("x", 80)+"\n")
	e.performRender()

	cols := glyphCols(out.String(), 'x')
	xs := cols[1] // the content row
	if len(xs) == 0 {
		t.Fatal("no document glyphs painted on the content row")
	}
	max := 0
	for _, c := range xs {
		if c > max {
			max = c
		}
	}
	if max > 42 {
		t.Fatalf("document glyphs reach column %d; the main viewport should stop near column 40 (left half of 80)", max)
	}
	if max < 30 {
		t.Fatalf("document glyphs only reach column %d; expected the main viewport to fill most of the left half", max)
	}

	// No 'x' anywhere in the right half, on any row.
	for r, list := range cols {
		for _, c := range list {
			if c > 42 {
				t.Fatalf("row %d painted a document glyph at column %d; the right (blank) half must stay empty", r, c)
			}
		}
	}

	// Sanity: the tiler was built and reports exactly [main blank].
	if e.tiler == nil {
		t.Fatal("tiler was not instantiated during render")
	}
	var refs []string
	for _, b := range e.tiler.Tiles() {
		refs = append(refs, b.Ref)
	}
	if strings.Join(refs, ",") != "main,blank" {
		t.Fatalf("tiler tiles = %v; want [main blank]", refs)
	}
}
