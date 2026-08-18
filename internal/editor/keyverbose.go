package editor

import (
	"strings"
	"unicode"

	"github.com/phroun/mew/internal/plugins"
)

// verboseKeyNames maps mew's internal key-token names to the beginner-facing
// spellings the keys_verbose# helper writes.
var verboseKeyNames = map[string]string{
	"esc":    "Esc",
	"escape": "Esc",
	"space":  "Space",
	"tab":    "Tab",
	// Every key gets its own word; none may borrow another's. return and enter
	// are two keys, and del and fdel are two keys.
	"return":    "Return",
	"enter":     "Enter",
	"back":      "Backspace",
	"backspace": "Backspace",
	"del":       "Delete",
	"fdel":      "FDel",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"ins":       "Insert",
	"pgup":      "Page Up",
	"pgdn":      "Page Down",
	// The keypad's 5 with NumLock off, which reaches a badge as "Keypad Begin".
	"begin": "Begin",
	"power": "Power",
	// The pad's lock cap, which only reaches a badge as a chord — pressed alone
	// it is not a key at all but the pad's lock, and that decides whether the
	// caps above it read "Keypad 7" or "Keypad Home". Named for what it does on
	// the keyboards with no lock behind it, which is where the legend says
	// Clear.
	"clear": "Clear",
	// Lock and system keys. mew's token runs the words together; a badge is
	// prose, so it reads them as the keycap prints them.
	"capslock":    "Caps Lock",
	"scrolllock":  "Scroll Lock",
	"printscreen": "Print Screen",
	"pause":       "Pause",
	"menu":        "Menu",
	// Keys an American keyboard does not have. mew spells them lowercase like
	// the rest of its vocabulary, but a badge is prose and these are proper
	// names — the capitalisation is upstream's, not invented here.
	"zig":        "Zig",
	"zag":        "Zag",
	"ro":         "Ro",
	"yen":        "Yen",
	"kanalock":   "KanaLock",
	"hangullock": "HangulLock",
	"henkan":     "Henkan",
	"muhenkan":   "Muhenkan",
	"hanja":      "Hanja",
}

// tfcKeyResolver builds a TFC (Text Format Control) resolver for the
// %keys#…% and %keys_verbose#…% codes: the code inside the %…% mirrors a
// [[keys#action|alias]] link — a "keys#"/"keys_verbose#" target, then a "|" and
// the fallback/alias key — and resolves to the live binding (spelled out for
// keys_verbose#). Each resolved binding is wrapped in open…close (ANSI the call
// site chooses; empty for none), so a badge can be colored where TFC is
// expanded. A non-keys# code returns ok=false, left verbatim by the engine.
func (e *Editor) tfcKeyResolver(open, closing string) plugins.TFCResolver {
	return func(code string) (string, bool) {
		target, alias := code, ""
		if i := strings.IndexByte(code, '|'); i >= 0 {
			target, alias = code[:i], code[i+1:]
		}
		action, verbose, ok := keysRefAction(target)
		if !ok {
			return "", false
		}
		disp := e.keyBindingDisplay(action, alias)
		if verbose {
			disp = e.verboseKeys(disp)
		}
		return open + disp + closing, true
	}
}

// verboseKeys renders a key binding in the long, beginner-facing form the
// keys_verbose# helper uses, consulting the live keymap for Shift
// disambiguation (see verboseKeySequence).
func (e *Editor) verboseKeys(seq string) string {
	isBound := func(s string) bool {
		return e.KeyProcessor != nil && e.KeyProcessor.GetMapping(s) != ""
	}
	return verboseKeySequence(seq, isBound)
}

// verboseKeySequence spells a space-separated binding (e.g. "^B O") out for
// beginners, for help pages written before the terse notation is introduced.
// Modifiers spell out — ^ and C- become "Ctrl+", s- "Super+", G- "Glyph+", H-
// "Hyper+", M- and m- both "Meta+" until both are bound (see metaSignificant),
// and Shift attaches to the base key as "Shift-" — and the keys of a chord are
// joined with "then", "followed by", and "and finally" (see joinVerboseTerms).
//
// Shift on a letter is shown only when it MATTERS: an explicit S- in the
// binding, or a letter whose case is significant — i.e. the same binding with
// that letter's case flipped is ALSO bound (both defined to disambiguate). The
// keybinding system otherwise case-folds letters, so their case implies no
// Shift. isBound reports whether a full sequence string is a live binding.
func verboseKeySequence(seq string, isBound func(string) bool) string {
	fields := strings.Fields(seq)
	if len(fields) == 0 {
		return seq
	}
	terms := make([]string, len(fields))
	for i, f := range fields {
		terms[i] = verboseKeyToken(f,
			caseSignificant(fields, i, isBound),
			metaSignificant(fields, i, isBound))
	}
	return joinVerboseTerms(terms)
}

// caseSignificant reports whether the case of fields[i]'s letter must be shown
// as Shift: the same sequence with that token's letter flipped is ALSO bound
// (both cases defined, so the case disambiguates two real bindings). A token
// with no single letter, or whose flipped form is unbound, is case-folded.
func caseSignificant(fields []string, i int, isBound func(string) bool) bool {
	if isBound == nil {
		return false
	}
	flipped, ok := flipTokenLetter(fields, i)
	if !ok {
		return false
	}
	return isBound(flipped)
}

// metaSignificant reports whether fields[i]'s token must name Mega or Micro
// rather than the friendlier "Meta".
//
// The two fall back to each other in the key sequence processor: bind one and
// either reaches it, bind both and they stay apart. So the moment a user needs
// to be told WHICH key is the moment both are bound — before that, telling them
// "Micro" would name a key most keyboards do not have, when pressing the one
// they do have works. This is the same shape as caseSignificant above, asking
// about the modifier prefix instead of the letter's case.
//
// A token carrying BOTH is significant whatever else is bound: "Meta+Meta+Home"
// would say one word for two keys held at once.
func metaSignificant(fields []string, i int, isBound func(string) bool) bool {
	prefix, _ := splitKeyToken(fields[i])
	mega := strings.Contains(prefix, "M-")
	micro := strings.Contains(prefix, "m-")
	if mega && micro {
		return true
	}
	if !mega && !micro || isBound == nil {
		return false
	}
	flipped, ok := flipTokenMeta(fields, i)
	if !ok {
		return false
	}
	return isBound(flipped)
}

// flipTokenMeta returns the full sequence with fields[i]'s Mega prefix swapped
// for Micro or the reverse, or ok=false when the token carries neither.
//
// The swap is in place, which keeps the stack in canonical order: "M-" and "m-"
// are adjacent in the rank (see keyseq's modifierRank), so nothing that sorted
// before or after one sorts differently against the other.
func flipTokenMeta(fields []string, i int) (string, bool) {
	prefix, base := splitKeyToken(fields[i])
	var flipped string
	switch {
	case strings.Contains(prefix, "M-"):
		flipped = strings.Replace(prefix, "M-", "m-", 1)
	case strings.Contains(prefix, "m-"):
		flipped = strings.Replace(prefix, "m-", "M-", 1)
	default:
		return "", false
	}
	out := make([]string, len(fields))
	copy(out, fields)
	out[i] = flipped + base
	return strings.Join(out, " "), true
}

// flipTokenLetter returns the full sequence with the letter of fields[i]'s base
// switched in case (Meta+b <-> Meta+B), or ok=false when that token has no
// single-letter base.
func flipTokenLetter(fields []string, i int) (string, bool) {
	prefix, base := splitKeyToken(fields[i])
	if !isSingleLetter(base) {
		return "", false
	}
	flipped := strings.ToUpper(base)
	if base == flipped {
		flipped = strings.ToLower(base)
	}
	out := make([]string, len(fields))
	copy(out, fields)
	out[i] = prefix + flipped
	return strings.Join(out, " "), true
}

// joinVerboseTerms joins chord terms into prose: "then" between the first two,
// "followed by" introducing the third, "then" for any further keys, and "and
// finally" before the last — but "and finally" is never used unless a "followed
// by" already preceded it (so two- and three-key chords never say it).
func joinVerboseTerms(terms []string) string {
	n := len(terms)
	if n == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(terms[0])
	for i := 1; i < n; i++ {
		last := i == n-1
		var sep string
		switch {
		case last && n >= 4:
			// The last key of a 4+-key chord: a "followed by" (at i==2) always
			// precedes it, so "and finally" is allowed.
			sep = " and finally "
		case i == 1:
			sep = " then "
		case i == 2:
			sep = " followed by "
		default:
			sep = " then "
		}
		b.WriteString(sep)
		b.WriteString(terms[i])
	}
	return b.String()
}

// verboseKeyToken renders one key token ("^B", "M-b", "S-tab"). caseSignificant
// says whether the base letter's case encodes a real Shift (both cases bound);
// metaSignificant says whether Mega and Micro must be named apart rather than
// both reading as the familiar "Meta".
func verboseKeyToken(tok string, caseSignificant, metaSignificant bool) string {
	prefix, base := splitKeyToken(tok)
	shift := strings.Contains(prefix, "S-") // explicit Shift in the binding

	// An uppercase letter means Shift only when its case is significant — the
	// lowercase binding also exists, so the two are told apart by Shift.
	if caseSignificant && isSingleLetter(base) && base == strings.ToUpper(base) {
		shift = true
	}

	var b strings.Builder
	written := ""
	for _, m := range verboseModifiers {
		word := m.word
		// Mega and Micro both read as "Meta" until something needs them apart.
		if !metaSignificant && (m.prefix == "M-" || m.prefix == "m-") {
			word = "Meta+"
		}
		// ^ and C- are the same modifier spelled two ways, so a token carrying
		// both says Ctrl once.
		if strings.Contains(prefix, m.prefix) && word != written {
			b.WriteString(word)
			written = word
		}
	}
	// Shift is written last because it attaches to the base key rather than
	// standing on its own ("Meta+Shift-B").
	if shift {
		b.WriteString("Shift-")
	}
	b.WriteString(verboseKeyBase(base))
	return b.String()
}

// verboseModifiers spells each modifier prefix out, in the order they are
// written. The prefixes are distinguished by case (M- is Mega, m- is Micro —
// two readings of the meta lineage, two different keys), so a Contains test
// never confuses one for the other. S- is absent: Shift is written last, glued
// to the base key.
//
// The words for "M-" and "m-" here are the DISAMBIGUATED ones. Neither is what
// a reader normally sees: verboseKeyToken substitutes "Meta+" for both unless
// metaSignificant says they have to be told apart.
//
// Mega and Micro are neutral names the libraries need — direct-key-handler,
// key-sequence-processor and kittytk are offered to anyone, and two keys have a
// real claim to "Meta", so those take neither (see core.MegaModifier). mew is
// not neutral: its default keymap binds one of them and not the other, and its
// reader has been told for forty years that the key under the Alt or Option cap
// is Meta. Naming it Mega on a badge would name something they have never seen,
// to settle a contest they are not in.
//
// The contest only reaches them when both keys are actually bound, and then
// the friendly word stops being usable — which is precisely the condition
// metaSignificant tests.
var verboseModifiers = []struct{ prefix, word string }{
	{"^", "Ctrl+"},
	{"C-", "Ctrl+"},
	{"G-", "Glyph+"},
	{"M-", "Mega+"},
	{"m-", "Micro+"},
	{"s-", "Super+"},
	{"H-", "Hyper+"},
	// The keypad, which reads as a place rather than a held key: "Keypad Home"
	// is what a person would say out loud, and the badge is for saying things
	// out loud. Both cases get the same word — the lowercase form marks which
	// of two duplicated pad keys a keyboard sent, a distinction that belongs in
	// a keymap and means nothing to someone reading a menu.
	{"P-", "Keypad "},
	{"p-", "Keypad "},
}

// keyModifierPrefixes is the modifier vocabulary in canonical order. Every
// entry is two characters and no two can match the same text, so at most one
// applies per pass and the order is for reading only. Membership is what
// matters: a prefix missing here is never peeled, so its token keeps the raw
// spelling ("C-x") where the whole point is to spell it out. (^ is one
// character and is handled separately.)
var keyModifierPrefixes = []string{"C-", "G-", "M-", "m-", "S-", "s-", "H-", "P-", "p-"}

// splitKeyToken peels the modifier prefixes (^ and keyModifierPrefixes) off a
// token, returning the accumulated prefix string and the bare base key.
func splitKeyToken(tok string) (prefix, base string) {
	base = tok
	for {
		matched := false
		for _, p := range keyModifierPrefixes {
			if strings.HasPrefix(base, p) {
				prefix, base = prefix+p, base[2:]
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Control prefix, only when something follows it.
		if strings.HasPrefix(base, "^") && len(base) > 1 {
			prefix, base = prefix+"^", base[1:]
			continue
		}
		return prefix, base
	}
}

// verboseKeyBase renders a bare key (no modifiers): a friendly name for a named
// key, an uppercased single letter, else the token unchanged (digits,
// punctuation, function keys).
func verboseKeyBase(base string) string {
	if v, ok := verboseKeyNames[strings.ToLower(base)]; ok {
		return v
	}
	if isSingleLetter(base) {
		return strings.ToUpper(base)
	}
	return base
}

// isSingleLetter reports whether s is exactly one ASCII letter.
func isSingleLetter(s string) bool {
	if len(s) != 1 {
		return false
	}
	r := rune(s[0])
	return r < 128 && unicode.IsLetter(r)
}
