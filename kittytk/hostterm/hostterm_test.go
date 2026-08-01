package hostterm

import "testing"

func TestDetectFrom(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Kind
	}{
		{"iterm2 by LC_TERMINAL", map[string]string{"LC_TERMINAL": "iTerm2", "TERM_PROGRAM": "iTerm.app"}, TerminalITerm2},
		{"iterm2 by TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "iTerm.app"}, TerminalITerm2},
		{"ghostty by TERM", map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty"}, TerminalGhostty},
		{"ghostty by TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "ghostty"}, TerminalGhostty},
		// TerminalKitty is recognised by its TERM even if TERM_PROGRAM is mis-set.
		{"kitty by TERM despite bad TERM_PROGRAM", map[string]string{"TERM": "xterm-kitty", "TERM_PROGRAM": "ghostty"}, TerminalKitty},
		{"apple terminal", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal"}, TerminalAppleTerminal},
		{"cool-retro-term by bundle", map[string]string{"__CFBundleIdentifier": "com.yourcompany.cool-retro-term"}, TerminalCoolRetroTerm},
		{"alacritty by socket", map[string]string{"ALACRITTY_SOCKET": "/tmp/Alacritty.sock", "__CFBundleIdentifier": "org.alacritty"}, TerminalAlacritty},
		{"alacritty by window id", map[string]string{"ALACRITTY_WINDOW_ID": "123"}, TerminalAlacritty},
		{"purfecterm", map[string]string{"TERM": "xterm-purfecterm", "TERM_PROGRAM": "purfecterm"}, TerminalPurfecterm},
		// Embedded sub-terminals advertise a REAL terminfo name so zsh/clear/vim
		// work; the purfecterm identity rides on TERM_PROGRAM, which must still
		// classify.
		{"purfecterm by TERM_PROGRAM with real TERM", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "purfecterm"}, TerminalPurfecterm},
		{"unknown", map[string]string{"TERM": "xterm-256color"}, TerminalUnknown},
		{"empty", map[string]string{}, TerminalUnknown},
	}
	for _, c := range cases {
		getenv := func(k string) string { return c.env[k] }
		if got := DetectFrom(getenv); got != c.want {
			t.Errorf("%s: DetectFrom = %v (%s), want %v (%s)", c.name, got, got, c.want, c.want)
		}
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{
		TerminalITerm2: "iterm2", TerminalGhostty: "ghostty", TerminalKitty: "kitty",
		TerminalAppleTerminal: "apple-terminal", TerminalCoolRetroTerm: "cool-retro-term",
		TerminalAlacritty: "alacritty", TerminalPurfecterm: "purfecterm", TerminalUnknown: "unknown",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
