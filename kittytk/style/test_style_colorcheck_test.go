package style

import "testing"

func TestColorConstantsStable(t *testing.T) {
	if ColorBlack != 0 || ColorWhite != 7 || ColorBrightWhite != 15 || ColorDefault != -1 || ColorTransparent != -2 {
		t.Fatalf("color constants shifted: black=%d white=%d bwhite=%d", ColorBlack, ColorWhite, ColorBrightWhite)
	}
}
