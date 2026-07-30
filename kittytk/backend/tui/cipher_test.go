package tui

import "testing"

// Each cipher pseudo-font maps A/a (and 0 when it has styled digits) to the
// right code points, fills the Letterlike holes, and passes everything else
// through. Reference values are the well-known math-alphanumeric starts.
func TestCipherText(t *testing.T) {
	cases := []struct {
		font string
		in   string
		want string
	}{
		{"Black Serif", "Ab0", "\U0001D400\U0001D41B\U0001D7CE"},   // Math Bold, has digits
		{"Black Sans", "Ab9", "\U0001D5D4\U0001D5EF\U0001D7F5"},    // Math Sans Bold, has digits
		{"Double-Struck", "Ab0", "\U0001D538\U0001D553\U0001D7D8"}, // has digits
		{"Double-Struck", "CHNPQRZ", "ℂℍℕℙℚℝℤ"},                    // all uppercase holes
		{"Fraktur", "CHIRZ", "ℭℌℑℜℨ"},                              // Fraktur uppercase holes
		{"Fraktur", "ab", "\U0001D51E\U0001D51F"},                  // Fraktur non-hole
		{"Bold Fraktur", "Aa", "\U0001D56C\U0001D586"},
		{"Bold Italic", "Aa", "\U0001D468\U0001D482"},
		{"Bold Script", "Aa", "\U0001D4D0\U0001D4EA"},
		{"Black Italic", "Aa", "\U0001D63C\U0001D656"},
		{"Italic", "Aa", "\U0001D608\U0001D622"},
		// Styles without styled digits leave 0-9 (and all punctuation) alone.
		{"Fraktur", "A1! z", "\U0001D504" + "1! " + "\U0001D537"},
		// Non-cipher names are identity.
		{"Monday", "Ab0", "Ab0"},
		{"Tuesday", "Ab0", "Ab0"},
		{"ui-term", "Hi!", "Hi!"},
	}
	for _, c := range cases {
		if got := cipherText(c.font, c.in); got != c.want {
			t.Errorf("cipherText(%q, %q) = %q, want %q", c.font, c.in, got, c.want)
		}
	}
}

// Aliases and case-insensitivity resolve to the same style.
func TestCipherAliases(t *testing.T) {
	same := cipherText("Double-Struck", "AZ")
	for _, alias := range []string{"double struck", "DOUBLE-STRUCK", "Double Struck"} {
		if got := cipherText(alias, "AZ"); got != same {
			t.Errorf("alias %q gave %q, want %q", alias, got, same)
		}
	}
	if cipherText("gothic", "C") != cipherText("Fraktur", "C") {
		t.Errorf("'gothic' should alias to Fraktur")
	}
}

// pseudofont_<group>=off renders that by-name cipher plain. fraktur_mode is a
// SEPARATE knob that does NOT gate the by-name fraktur cipher.
func TestCipherGating(t *testing.T) {
	defer ConfigurePseudoFonts(nil, FrakturPseudo) // restore

	// Disable the fraktur cipher by name: "Fraktur"/"Bold Fraktur" go plain.
	ConfigurePseudoFonts(map[string]bool{"fraktur": true}, FrakturPseudo)
	if got := cipherText("Fraktur", "AB"); got != "AB" {
		t.Errorf("disabled fraktur cipher should be plain, got %q", got)
	}
	if got := cipherText("Bold Fraktur", "AB"); got != "AB" {
		t.Errorf("disabled fraktur group covers Bold Fraktur, got %q", got)
	}
	if cipherText("Black Serif", "A") == "A" {
		t.Errorf("Black Serif should still cipher when only fraktur is off")
	}

	// fraktur_mode is orthogonal to the by-name cipher toggle: with the cipher
	// ENABLED, native/off modes must NOT stop the by-name cipher; the mode is
	// exposed separately for the VT request path.
	ConfigurePseudoFonts(nil, FrakturNative)
	if cipherText("Fraktur", "A") == "A" {
		t.Errorf("fraktur_mode=native must not disable the by-name fraktur cipher")
	}
	if FrakturMode() != FrakturNative {
		t.Errorf("FrakturMode should be native, got %q", FrakturMode())
	}
	if !PseudoFontEnabled("fraktur") {
		t.Errorf("fraktur cipher group should be enabled")
	}
}

// VTFRAKTUR (the VT100 font-20 request) is governed by fraktur_mode, NOT by the
// pseudofont_fraktur by-name toggle. pseudo ciphers; off/native leave the
// characters plain (native additionally flags the cells for real SGR-20).
func TestVTFraktur(t *testing.T) {
	defer ConfigurePseudoFonts(nil, FrakturPseudo)
	frak := cipherText("Fraktur", "AB") // reference cipher output

	// pseudo: VTFRAKTUR ciphers like Fraktur, even with the by-name cipher OFF.
	ConfigurePseudoFonts(map[string]bool{"fraktur": true}, FrakturPseudo)
	if got := cipherText("VTFRAKTUR", "AB"); got != frak {
		t.Errorf("VTFRAKTUR pseudo should cipher regardless of pseudofont_fraktur, got %q", got)
	}
	if vtFrakturNative("VTFRAKTUR") {
		t.Errorf("pseudo mode is not native")
	}

	// off: plain, not native.
	ConfigurePseudoFonts(nil, FrakturOff)
	if got := cipherText("VTFRAKTUR", "AB"); got != "AB" {
		t.Errorf("VTFRAKTUR off should be plain, got %q", got)
	}
	if vtFrakturNative("VTFRAKTUR") {
		t.Errorf("off mode is not native")
	}

	// native: plain characters, but flagged for the real SGR-20 attribute.
	ConfigurePseudoFonts(nil, FrakturNative)
	if got := cipherText("VTFRAKTUR", "AB"); got != "AB" {
		t.Errorf("VTFRAKTUR native leaves characters plain, got %q", got)
	}
	if !vtFrakturNative("VTFRAKTUR") || !vtFrakturNative("vtfraktur") {
		t.Errorf("native mode should flag VTFRAKTUR (case-insensitive)")
	}
	if vtFrakturNative("Fraktur") {
		t.Errorf("the by-name Fraktur cipher is never the native VT request")
	}
}

// Every advertised cipher font name actually resolves (guards the display list
// against a typo drifting from the style table).
func TestCipherFontNamesResolve(t *testing.T) {
	for _, name := range CipherFontNames {
		if _, ok := lookupCipherStyle(name); !ok {
			t.Errorf("advertised cipher font %q does not resolve to a style", name)
		}
	}
}
