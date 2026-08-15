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
	// fdel has no long spelling on purpose. "delete" used to be one, which put
	// mew's two erase names one suffix apart while meaning opposite
	// directions — "del" erasing behind and "delete" ahead — the same
	// near-homograph the vocabulary is otherwise built to avoid. Forward
	// delete is spelled "fdel", and only that.
	// NUL is what Ctrl+Space and Ctrl+@ both send, so the legacy wire cannot
	// tell them apart and all three spellings name that one input. "^@" is
	// the one the byte path actually emits; without it a "^space" binding was
	// reachable only under the kitty protocol, which reports the key.
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
		// Insert a single typed character. A longer key is an unmapped named
		// or modified key (e.g. "F1", "ins", "pgup").
		if len([]rune(key)) == 1 {
			return "insert '" + escapeStringLiteral(key) + "'"
		}
		// A Glyph chord (the AltGr/Level3 modifier, prefix "G-") that no
		// binding claimed inserts the composed character it carries: the
		// graphical host forms the token from the AltGr-composed glyph, so
		// "G-€" types "€" by default while a user can still bind e.g.
		// `G-€ = insert 'EUR'` to intercept it. Unrolling is just dropping the
		// prefix — the glyph rides in the token itself, so no lookup is needed.
		if strings.HasPrefix(key, "G-") {
			if glyph := key[2:]; len([]rune(glyph)) == 1 {
				return "insert '" + escapeStringLiteral(glyph) + "'"
			}
		}
		// An unmapped Meta combination types the character macOS Option would
		// have produced, so bindings steal individual Option combos while the
		// rest insert seamlessly (and Alt on any platform gains the same
		// mac-style character layer). The processor holds the table and the
		// on/off switch — a keyboard fact — and reports nothing when the layer
		// is off; spelling it as a command is mew's part.
		if ch, ok := e.KeyProcessor.MacOptionChar(key); ok {
			return "insert '" + escapeStringLiteral(ch) + "'"
		}
		return ""
	}
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
