package style

import (
	"strings"
	"testing"
)

// A 256-color terminal (macOS Terminal.app: TERM=xterm-256color, no
// COLORTERM) must never receive 24-bit "38;2" sequences — it ignores them
// and the whole UI renders monochrome. RGB colors quantize to the xterm 256
// palette; basic colors pass through untouched.
func TestCodeDepth256QuantizesRGB(t *testing.T) {
	s := DefaultStyle().WithFg(RGB(32, 32, 32)).WithBg(RGB(238, 238, 238))
	code := s.CodeDepth(256)
	if strings.Contains(code, ";2;") {
		t.Fatalf("256-depth code still contains truecolor SGR: %q", code)
	}
	if !strings.Contains(code, "38;5;") || !strings.Contains(code, "48;5;") {
		t.Fatalf("256-depth code should use the 256 palette: %q", code)
	}

	// Pure gray tones land on the grayscale ramp (232..255), saturated
	// colors on the cube (16..231).
	if idx := rgbTo256(8, 8, 8); idx < 232 {
		t.Fatalf("near-black gray should map to the gray ramp, got %d", idx)
	}
	if idx := rgbTo256(255, 0, 0); idx != 196 {
		t.Fatalf("pure red should map to cube index 196, got %d", idx)
	}
	if idx := rgbTo256(0, 255, 0); idx != 46 {
		t.Fatalf("pure green should map to cube index 46, got %d", idx)
	}

	// Basic ANSI colors are untouched at any depth.
	if got := ColorGreen.FgCodeDepth(256); got != ColorGreen.FgCode() {
		t.Fatalf("basic color changed at 256 depth: %q", got)
	}
}

// A 16-color terminal gets everything as the basic ANSI colors.
func TestCodeDepth16(t *testing.T) {
	if got := RGB(250, 10, 10).FgCodeDepth(16); got != "\033[91m" {
		t.Fatalf("bright red RGB at depth 16: %q, want ^[[91m", got)
	}
	if got := RGB(0, 0, 0).BgCodeDepth(16); got != "\033[40m" {
		t.Fatalf("black RGB bg at depth 16: %q, want ^[[40m", got)
	}
	// A 256-palette index quantizes down through its RGB expansion.
	if got := Color256(196).FgCodeDepth(16); got != "\033[91m" {
		t.Fatalf("palette red at depth 16: %q, want ^[[91m", got)
	}
}

// Truecolor depth preserves today's emission exactly; monochrome drops
// color codes but keeps attributes.
func TestCodeDepthEnds(t *testing.T) {
	s := DefaultStyle().WithFg(RGB(1, 2, 3))
	if s.CodeDepth(TrueColorDepth) != s.Code() {
		t.Fatal("truecolor depth must match Code()")
	}
	mono := DefaultStyle().WithFg(RGB(200, 0, 0)).Bold().CodeDepth(2)
	if strings.Contains(mono, "38;") || strings.Contains(mono, ";2;") {
		t.Fatalf("monochrome depth must drop colors: %q", mono)
	}
	if !strings.Contains(mono, "1m") && !strings.Contains(mono, ";1") {
		t.Fatalf("monochrome depth must keep attributes: %q", mono)
	}
}
