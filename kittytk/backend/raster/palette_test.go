package raster

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/style"
)

// The backend resolves the 16 standard colors and the default fg/bg
// from the active theme palette, not a fixed assumed set.
func TestBackendUsesActiveTermPalette(t *testing.T) {
	b, err := New(64, 32)
	if err != nil {
		t.Fatal(err)
	}

	// ColorBlue is ANSI index 4, which the dark theme maps to its
	// dark-blue override #1846C8.
	if got, want := b.rgba(style.ColorBlue, true), (color.RGBA{0x18, 0x46, 0xC8, 255}); got != want {
		t.Errorf("ColorBlue = %v, want %v", got, want)
	}
	// ColorRed is ANSI index 1 = VGA dark red #C30E49 (base).
	if got, want := b.rgba(style.ColorRed, true), (color.RGBA{0xC3, 0x0E, 0x49, 255}); got != want {
		t.Errorf("ColorRed = %v, want %v", got, want)
	}
	// Default fg/bg track the active theme's foreground/background.
	if got, want := b.rgba(style.ColorDefault, false), (color.RGBA{0x00, 0x1E, 0x18, 255}); got != want {
		t.Errorf("ColorDefault bg = %v, want %v", got, want)
	}
	if got, want := b.rgba(style.ColorDefault, true), (color.RGBA{0xD4, 0xD4, 0xD4, 255}); got != want {
		t.Errorf("ColorDefault fg = %v, want %v", got, want)
	}
}
