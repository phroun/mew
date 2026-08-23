package editor

import "strings"

// textEventPrefix marks a PURE TEXT EVENT from the key layer: text the terminal
// received with no key behind it (direct-key-handler's keyboard.TextPrefix).
//
// The "kitty" keyboard protocol reports one as keycode 0, and an input method
// committing a composition is what sends it — the terminal owns the candidate
// list, so all that reaches this process is the text that was chosen.
//
// It is prefixed for the same reason CPR: and DECRPM: are: a bare name in that
// stream means a key was PRESSED, and no key was. Spelled as a key it would
// also have to be a KeyName, which a commit is not — any length and any
// content, so "今日" collides with the spelling grammar and, being two runes,
// was silently dropped by the one-rune rule that decides what an unbound key
// types.
const textEventPrefix = "Text:"

// handleCommittedText consumes a text event, putting the text in the document
// and reporting whether it took it.
//
// Committed through preedit_commit rather than insert, which is the same
// command the graphical host's compositions end with. Nothing is standing over
// anything here — a terminal cannot report a composition in flight, the
// protocol has no way to say it — so it lands as an ordinary insert. Using the
// one command keeps the two hosts on one path rather than two that have to be
// kept in step.
func (e *Editor) handleCommittedText(key string) bool {
	text, ok := strings.CutPrefix(key, textEventPrefix)
	if !ok {
		return false
	}
	if text == "" {
		// The key layer already drops an empty one; consumed here too rather
		// than passed on to be typed as a name.
		return true
	}
	// Held across the mutation for the reason dispatchKey holds it: the
	// renderer's resize goroutine reads this same state.
	e.renderMu.Lock()
	e.executeCommand("preedit_commit '" + escapeStringLiteral(text) + "'")
	e.renderMu.Unlock()
	return true
}
