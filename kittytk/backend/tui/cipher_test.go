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

// A disabled toggle group renders that style plain; real_fraktur is an
// independent knob that does NOT gate the cipher.
func TestCipherGating(t *testing.T) {
	defer ConfigurePseudoFonts(nil, false) // restore

	// Disable fraktur: "Fraktur"/"Bold Fraktur" go plain; others unaffected.
	ConfigurePseudoFonts(map[string]bool{"fraktur": true}, false)
	if got := cipherText("Fraktur", "AB"); got != "AB" {
		t.Errorf("disabled fraktur should be plain, got %q", got)
	}
	if got := cipherText("Bold Fraktur", "AB"); got != "AB" {
		t.Errorf("disabled fraktur group covers Bold Fraktur, got %q", got)
	}
	if cipherText("Black Serif", "A") == "A" {
		t.Errorf("Black Serif should still cipher when only fraktur is off")
	}

	// real_fraktur is orthogonal: with fraktur ENABLED, turning real_fraktur on
	// must NOT stop the cipher (both knobs independent), and it exposes the
	// passthrough decision.
	ConfigurePseudoFonts(nil, true)
	if cipherText("Fraktur", "A") == "A" {
		t.Errorf("real_fraktur must not disable the fraktur cipher")
	}
	if !RealFrakturPassthrough() {
		t.Errorf("RealFrakturPassthrough should be true")
	}
	if !PseudoFontEnabled("fraktur") {
		t.Errorf("fraktur group should be enabled")
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
