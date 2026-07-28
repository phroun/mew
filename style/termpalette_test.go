package style

import "testing"

func TestTermPaletteResolvesOverrides(t *testing.T) {
	// Base value survives where no override applies (VGA 4 = dark red).
	if got, want := TermPaletteDark.Colors[4], (TermRGB{0xC3, 0x0E, 0x49}); got != want {
		t.Errorf("dark VGA[4] = %v, want base %v", got, want)
	}
	// Dark override applies (VGA 1 = dark blue -> #1846C8).
	if got, want := TermPaletteDark.Colors[1], (TermRGB{0x18, 0x46, 0xC8}); got != want {
		t.Errorf("dark VGA[1] = %v, want override %v", got, want)
	}
	// Light override differs from dark (VGA 1 -> #4460B2).
	if got, want := TermPaletteLight.Colors[1], (TermRGB{0x44, 0x60, 0xB2}); got != want {
		t.Errorf("light VGA[1] = %v, want override %v", got, want)
	}
	// Background / foreground come from the per-theme entries.
	if got, want := TermPaletteDark.Background, (TermRGB{0x00, 0x1E, 0x18}); got != want {
		t.Errorf("dark background = %v, want %v", got, want)
	}
	if got, want := TermPaletteLight.Foreground, (TermRGB{0x1E, 0x1E, 0x1E}); got != want {
		t.Errorf("light foreground = %v, want %v", got, want)
	}
}

// The VGA->ANSI reorder swaps red and blue positions: ANSI blue (4)
// must pick up VGA dark blue (1); ANSI red (1) must pick up VGA dark
// red (4).
func TestTermPaletteANSIReorder(t *testing.T) {
	ansi := TermPaletteDark.ANSIColors()
	if got, want := ansi[4], TermPaletteDark.Colors[1]; got != want {
		t.Errorf("ANSI[4] (blue) = %v, want VGA[1] %v", got, want)
	}
	if got, want := ansi[1], TermPaletteDark.Colors[4]; got != want {
		t.Errorf("ANSI[1] (red) = %v, want VGA[4] %v", got, want)
	}
	// Black and white are fixed points of the swap.
	if ansi[0] != TermPaletteDark.Colors[0] || ansi[15] != TermPaletteDark.Colors[15] {
		t.Error("ANSI reorder disturbed black/white endpoints")
	}
}

func TestActiveTermThemeToggle(t *testing.T) {
	if ActiveTermPalette != TermPaletteDark {
		t.Fatal("default active palette should be dark")
	}
	defer SetActiveTermTheme(TermThemeDark) // restore for other tests

	SetActiveTermTheme(TermThemeLight)
	if ActiveTermPalette != TermPaletteLight {
		t.Error("after toggle, active palette should be light")
	}
	if ActiveTermTheme() != TermThemeLight {
		t.Error("ActiveTermTheme did not report light")
	}

	SetActiveTermTheme(TermThemeDark)
	if ActiveTermPalette != TermPaletteDark {
		t.Error("after toggle back, active palette should be dark")
	}
}
