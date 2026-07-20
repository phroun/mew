package config

import "testing"

// The link/button color slots resolve at the global level, and the button
// indicator glyphs parse from [indicators] with the shipped defaults.
func TestLinkButtonColorAndIndicatorSlots(t *testing.T) {
	m := &Manager{configDir: "/nowhere"}
	cfg := m.LoadFromString("[indicators]\nfocusedButtonLeft=\"[\"\n")
	for name, want := range map[string]string{
		"link":                "\x1b[0;4;93;40m",
		"linkRecent":          "\x1b[0;4;32;40m",
		"button":              "\x1b[0;30;47m",
		"buttonShadow":        "\x1b[0;90;47m",
		"buttonFocused":       "\x1b[0;30;46m",
		"buttonShadowFocused": "\x1b[0;90;46m",
	} {
		if got := cfg.Colors.Resolve("", "main", name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	ind := cfg.Indicators
	if ind.ButtonLeft != " " || ind.ButtonRight != " " || ind.ButtonShadow != "▐" {
		t.Fatalf("button glyph defaults wrong: %+v", ind)
	}
	if ind.FocusedButtonLeft != "[" {
		t.Fatal("[indicators] focusedButtonLeft override not applied")
	}
	if ind.FocusedButtonRight != ">" || ind.FocusedButtonShadow != "█" {
		t.Fatal("focused button glyph defaults wrong")
	}
}
