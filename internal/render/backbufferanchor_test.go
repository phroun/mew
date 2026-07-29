package render

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The renderer's column budget and the BACK BUFFER's cell occupancy must be
// the same number. prepareLineForDisplay promises `width` columns; if the back
// buffer ends up with fewer painted cells, the tail keeps its default style
// and the terminal's own background shows through the end of the line.
func TestBackBufferOccupancyMatchesBudget(t *testing.T) {
	const nko = "߭"
	cases := []string{
		"plain line, nothing unusual",
		nko + "an NKo tone opens this one",
		"Lorem\x01 ipsum" + nko + " dolor sit amet",
		"x́y a well-formed acute",
		"日֗ hebrew accent on a CJK base",
		"three " + nko + "marks " + nko + "in one " + nko + "line",
	}
	for _, line := range cases {
		sr, w := testRenderer()
		w.ViewState.Direction = ""
		const width = 40
		disp := sr.prepareLineForDisplay(line, "\n", width, 0, w, 0, selectionRange{}, nil, nil)

		b := &backBuffer{}
		b.reshape(width, 3)
		b.begin()
		b.moveTo(0, 0)
		b.writeString(disp)

		painted := 0
		for x := 0; x < width; x++ {
			c := b.cur[0][x]
			if len(c.runes) > 0 || c.style != "" || c.cont {
				painted = x + 1
			}
		}
		if painted != width {
			t.Errorf("%q: back buffer holds %d painted cells, budget was %d (tail keeps the terminal's own background)",
				line, painted, width)
		}
		if b.penX != width {
			t.Errorf("%q: pen ended at column %d, budget was %d", line, b.penX, width)
		}
	}
	_ = viewport.Viewport{}
}
