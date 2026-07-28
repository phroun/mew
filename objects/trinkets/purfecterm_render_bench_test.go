package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// BenchmarkTerminalRenderColored renders a full screen of unique-color
// half-block cells (doomfire-like) through the real graphical paint path,
// measuring steady-state render cost (the coverage-mask cache is warm). Use
// -cpuprofile to see where the per-frame time goes.
func BenchmarkTerminalRenderColored(b *testing.B) {
	be, err := raster.New(800, 600)
	if err != nil {
		b.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(be)
	term := NewPurfecTerm()
	if term.terminal == nil {
		b.Skip("terminal unavailable")
	}
	term.SetParent(d)
	term.SetBounds(core.UnitRect{Width: 640, Height: 384}) // ~80x24 at 8x16

	// A full frame of truecolor upper-half blocks.
	var sb []byte
	sb = append(sb, "\x1b[2J\x1b[H"...)
	for row := 0; row < 24; row++ {
		for col := 0; col < 80; col++ {
			r, g, bl := (col*3+row*7)%256, (col*5+13)%256, (row*11+col)%256
			sb = append(sb, fmt.Sprintf("\x1b[38;2;%d;%d;%dm▀", r, g, bl)...)
		}
		sb = append(sb, '\r', '\n')
	}
	term.Feed(sb)

	p := core.NewPainter(be)
	bnds := term.Bounds()
	term.paintGraphical(p, bnds) // warm the mask cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		term.paintGraphical(p, bnds)
	}
}
