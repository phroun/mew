// Package hostterm identifies the terminal emulator KittyTK (or an application
// built on it) is running inside, from the process environment. It is detected
// ONCE, lazily, and cached, so callers can branch on the result cheaply wherever
// terminal-specific behaviour is needed.
//
// It is deliberately dependency-free (standard library only) so it can be
// contributed upstream and imported from anywhere without pulling in the rest of
// the toolkit.
package hostterm

import (
	"os"
	"strings"
	"sync"
)

// The TERM and TERM_PROGRAM values KittyTK's embedded terminal (purfecterm)
// advertises to the processes it spawns, so a program running inside it —
// including a nested instance of the app — detects Purfecterm. These are the
// single source of truth for both setting the child environment and detecting
// it (see detect).
const (
	PurfectermTerm        = "xterm-purfecterm"
	PurfectermTermProgram = "purfecterm"
)

// Kind is a known host terminal emulator (or Unknown).
type Kind int

// Names are prefixed with Terminal to keep them clearly about terminal
// emulators, distinct from KittyTK (this toolkit).
const (
	TerminalUnknown Kind = iota
	TerminalITerm2
	TerminalGhostty
	TerminalKitty
	TerminalAppleTerminal
	TerminalCoolRetroTerm
	TerminalAlacritty
	TerminalPurfecterm // KittyTK's own embedded terminal (TERM_PROGRAM=purfecterm)
	TerminalSDL        // the graphical SDL host: renders natively, not via a terminal
)

// String is a short stable identifier, handy for logs and config display.
func (k Kind) String() string {
	switch k {
	case TerminalITerm2:
		return "iterm2"
	case TerminalGhostty:
		return "ghostty"
	case TerminalKitty:
		return "kitty"
	case TerminalAppleTerminal:
		return "apple-terminal"
	case TerminalCoolRetroTerm:
		return "cool-retro-term"
	case TerminalAlacritty:
		return "alacritty"
	case TerminalPurfecterm:
		return "purfecterm"
	case TerminalSDL:
		return "sdl"
	default:
		return "unknown"
	}
}

// detect classifies the host terminal from an environment lookup. It takes the
// getenv function so it can be tested without touching the real environment.
//
// The order matters: the most specific / least spoofable signals are checked
// first. TERM values (xterm-<name>) are preferred over TERM_PROGRAM where a
// terminal sets a reliable one, since some emulators inherit or mis-set
// TERM_PROGRAM.
func detect(getenv func(string) string) Kind {
	term := getenv("TERM")
	prog := getenv("TERM_PROGRAM")
	bundle := getenv("__CFBundleIdentifier")

	switch {
	case getenv("LC_TERMINAL") == "iTerm2" || prog == "iTerm.app":
		return TerminalITerm2
	case term == "xterm-kitty":
		return TerminalKitty
	case term == "xterm-ghostty" || prog == "ghostty":
		return TerminalGhostty
	case term == PurfectermTerm || prog == PurfectermTermProgram:
		return TerminalPurfecterm
	case prog == "Apple_Terminal":
		return TerminalAppleTerminal
	case getenv("ALACRITTY_SOCKET") != "" || getenv("ALACRITTY_WINDOW_ID") != "" || bundle == "org.alacritty":
		return TerminalAlacritty
	case strings.Contains(bundle, "cool-retro-term") || strings.Contains(getenv("COLORSCHEMES_DIR"), "cool-retro-term"):
		return TerminalCoolRetroTerm
	}
	return TerminalUnknown
}

var (
	once     sync.Once
	detected Kind
)

// Detect returns the host terminal, computed once from the process environment
// and cached for the lifetime of the process.
func Detect() Kind {
	once.Do(func() { detected = detect(os.Getenv) })
	return detected
}

// Override pins the host kind, overriding environment detection for the whole
// process. The graphical SDL host uses it so it reports its own identity
// (TerminalSDL) rather than the terminal it happened to be launched from —
// whose flip/fold rendering quirks do not apply to native SDL drawing. Embedded
// sub-terminals run as separate processes that advertise TERM_PROGRAM=purfecterm
// (with a real TERM, xterm-256color, so terminfo-strict programs work) and
// detect normally. Safe before or after the first Detect(); the pinned value wins.
func Override(k Kind) {
	once.Do(func() {}) // consume lazy init so a later Detect() cannot overwrite
	detected = k
}

// DetectFrom classifies from an arbitrary environment lookup without caching —
// for tests and callers that need to evaluate a synthetic environment.
func DetectFrom(getenv func(string) string) Kind { return detect(getenv) }

// BidiProfile reports whether a terminal runs its OWN bidirectional reordering
// over what it is sent, how it segments when it does, and whether this package
// knows the answer for that terminal at all.
//
// It matters to anything that has already ORDERED a line. A renderer holding a
// line in visual order -- right-to-left words turned over, ready to be stamped
// left to right -- has its work undone by a terminal that orders it again, so
// on such a host each right-to-left run has to go out turned back for the
// terminal's own pass to turn forward. Sent to a terminal that leaves what it
// is given alone, that turning-back is itself the bug; so the answer has to be
// per-terminal, and unknown is its own answer rather than a guess.
//
// macOS Terminal.app is the one in common use that reorders. It reverses a
// whole parsed span, attributes and all, so wordwise is off for it.
func BidiProfile(k Kind) (applies, wordwise, known bool) {
	switch k {
	case TerminalAppleTerminal:
		return true, false, true
	case TerminalITerm2, TerminalGhostty, TerminalAlacritty, TerminalKitty,
		TerminalCoolRetroTerm, TerminalPurfecterm, TerminalSDL:
		// Stream order: what is sent is what appears.
		return false, false, true
	}
	return false, false, false
}
