package trinkets

import (
	"encoding/base64"
	"testing"
)

// OSC 52 is how a program running inside this terminal reaches the system
// clipboard — vim's "+y, tmux's set-clipboard, a mew hosted in an exec
// session. The trinket bridges it to the desktop's clipboard.
//
// With no desktop above it there is nowhere to put the text, and the only
// requirement is that it be a quiet no-op: a terminal built for measurement
// or in a test has no ancestry, and a copy in one must not take the process
// down.
func TestOSC52WithoutADesktopIsQuiet(t *testing.T) {
	term := NewPurfecTerm()
	if term.terminal == nil {
		t.Skip("no emulator")
	}
	enc := base64.StdEncoding.EncodeToString([]byte("hello"))
	term.Feed([]byte("\x1b]52;c;" + enc + "\x1b\\")) // write
	term.Feed([]byte("\x1b]52;c;\x1b\\"))            // clear
	term.Feed([]byte("\x1b]52;c;?\x1b\\"))           // query

	// And nothing of the sequences reached the screen — the ST form used to
	// leave a stray backslash behind, before purfecterm v0.2.28.
	cells := term.terminal.GetCells()
	for y, row := range cells {
		for x, c := range row {
			if c.Char != 0 && c.Char != ' ' {
				t.Fatalf("OSC 52 left %q on screen at %d,%d", c.Char, x, y)
			}
		}
	}
}

// Clipboard READS are denied unless the app opts in: a query lets anything
// that can print to the terminal read the user's clipboard. The opt-in exists
// for apps that want it, and neither direction may panic without a desktop.
func TestClipboardReadOptIn(t *testing.T) {
	term := NewPurfecTerm()
	if term.terminal == nil {
		t.Skip("no emulator")
	}
	if got := term.terminal.Buffer(); got == nil {
		t.Fatal("no buffer")
	}
	// Default: denied. The guest is answered (empty), not left hanging.
	term.Feed([]byte("\x1b]52;c;?\x1b\\"))

	term.SetClipboardReadAllowed(true)
	term.Feed([]byte("\x1b]52;c;?\x1b\\"))

	term.SetClipboardReadAllowed(false)
	term.Feed([]byte("\x1b]52;c;?\x1b\\"))
}
