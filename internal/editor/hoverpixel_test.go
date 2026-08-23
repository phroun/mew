package editor

import (
	"strings"
	"testing"
)

// Under SGR-Pixels (?1016) a plain-motion report (Mouse@) carries PIXELS.
// Hover must use the position already pixel-converted at the top of
// handleMouseKey, not re-parse the raw report as cells — otherwise the hover
// lands far off the grid and the button never lights (the cursor still changes
// via a separate path, which is what made this sneaky).
func TestHoverUsesPixelConvertedPosition(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	w.BrowseActive = true
	e.performRender()

	// Activate pixel reporting with a known cell size.
	const cw, ch = 10, 20
	e.pixelMouse = pixelMouseState{phase: pixelMouseActive, cellW: cw, cellH: ch}

	// Aim at the button (screen cell 5 on the content row), expressed in PIXELS
	// at the middle of that cell.
	screenCol := w.ContentX + 1 + 5
	screenRow := w.ContentY + 1
	px := (screenCol-1)*cw + cw/2
	py := (screenRow-1)*ch + ch/2
	e.handleMouseKey("Mouse@" + itoa(px) + "," + itoa(py))

	if !e.mouseHovered.active {
		t.Fatal("hover should latch over the button when the report is in pixels")
	}
	if out := renderTo(e); !strings.Contains(out, "\x1b[0;93;45m") {
		t.Fatal("the hovered button should paint in buttonHover")
	}

	// A pixel position off the button clears the hover.
	offCol := w.ContentX + 1 + 15
	e.handleMouseKey("Mouse@" + itoa((offCol-1)*cw+cw/2) + "," + itoa(py))
	if e.mouseHovered.active {
		t.Fatal("hover should clear when the pixel position is off the button")
	}
}
