package editor

import (
	"strings"

	"github.com/phroun/key-sequence-processor/keyseq"
)

// mewFallbackGroups list the key tokens that FALL BACK to each other in mew's
// vocabulary. They are not declared identical: the token as pressed is matched
// first, so binding two members of a group keeps them apart, and a group only
// says what to try next when the pressed token has no binding. The first entry
// is the name a key arrives under.
//
// These are mew's, not the processor's defaults: those speak
// direct-key-handler's names (Tab, Return, Escape), while mew renames keys on
// the way in (see internal/input) to the short lowercase tokens its binding
// syntax and help topics are written in. Without this the control spellings
// stop resolving — `^M` would not reach a `return` binding — silently, since
// nothing errors when a binding merely fails to match.
var mewFallbackGroups = []keyseq.FallbackGroup{
	{"back", "^H", "backspace"},
	{"tab", "^I"},
	{"return", "enter", "^M"},
	// fdel is absent, and deliberately: it has no long spelling. "delete" used
	// to be one, which put mew's two erase names one suffix apart while meaning
	// opposite directions — "del" erasing behind and "delete" ahead — the same
	// near-homograph the vocabulary is otherwise built to avoid. Forward delete
	// is written "fdel", and only that.

	// NUL is what Ctrl+Space and Ctrl+@ both send, so the legacy wire cannot
	// tell those two chords apart, and this group is the fallback between them.
	// "^@" and "^2" are KeyNames — things that actually arrive — while "^space"
	// is a Spelling, a way to write the binding that nothing ever emits. "^@"
	// is what the byte path delivers; without it here, a "^space" binding was
	// reachable only under the kitty protocol, which reports the key instead.
	{"^@", "^2", "^space"},
	{"esc", "escape", "^[", "^3"},
	{"^\\", "^4"},
	{"^]", "^5"},
	{"^^", "^6"},
	{"^_", "^7"},
	{"del", "^8"},

	// Word spellings for punctuation, so a binding can name a key the syntax
	// it is written in would otherwise fight over. `-` is the modifier
	// separator, so `M--` reads badly; `M-minus` says it plainly. Nothing ever
	// arrives under these names — they are for the keymap side only.
	{"-", "minus"},
	{"+", "plus"},
	{"=", "equals"},
	{"'", "apos"},
	{"\"", "quote"},
	{"~", "tilde", "wave"},
	{"`", "backtick"},
	{"\\", "backslash"},
	{"/", "slash"},
	{";", "semicolon"},
	{":", "colon"},
	{"|", "pipe"},
	{",", "comma"},
	{".", "period", "dot"},
	{"#", "octothorpe"},
}

// defaultCommandForKey answers what a key no binding claimed should do — the
// floor the key sequence processor falls to once every precedence level has
// declined (keyseq.DefaultHandler).
//
// It lives here, in mew, rather than in the processor because every answer is
// written in mew's command vocabulary: another application's floor would spell
// the same intents with its own verbs. The processor supplies the resolution
// rules and the keyboard facts; this supplies the meaning.
//
// A binding always outranks it, so every default below is a fallback a user
// can take back by mapping the key.
func (e *Editor) defaultCommandForKey(key string) string {
	switch key {
	case "space":
		return "insert ' '"
	case "del", "back":
		return "nav_history_prior false|del_char_prior"
	case "return":
		return "nav_follow false|accept|insert_newline"
	case "^C":
		return "cancel|viewport_close"
	case "esc":
		return "cmd"
	default:
		// What an unbound key TYPES, which the processor answers because every
		// step of it is a keyboard fact: what the host watched this keyboard
		// produce, what a one-character name carries, what a Glyph token
		// carries, and what the macOS Option layer says. Spelling it as a
		// command is mew's part — the vocabulary is mew's and the keyboard is
		// not.
		//
		// Known with EMPTY text is an answer: the key types nothing. A dead key
		// is that, and reading it as "no answer" is what put an armed accent
		// into the document ahead of the character it composed.
		if text, known := e.KeyProcessor.TextForKey(key); known {
			if text == "" {
				return ""
			}
			return "insert '" + escapeStringLiteral(text) + "'"
		}
		return ""
	}
}

// isBareModifierKey reports whether a key name is a modifier reporting ITSELF
// rather than a chord: "LMod:S" (left Shift), "RMod:C" (right Control),
// "Mod:H" (a Hyper whose side the producer could not tell).
//
// The suffix is the modifier's own prefix letter, and the side leads because
// which cap it was is the only thing such an event has to say.
func isBareModifierKey(key string) bool {
	for _, p := range []string{"LMod:", "RMod:", "Mod:"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// escapeStringLiteral escapes a string for use in a PawScript command string
// literal, so a typed quote or backslash cannot break out of the insert.
func escapeStringLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
