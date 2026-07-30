package editor

import (
	"strings"
	"unicode"

	"github.com/phroun/mew/internal/plugins"
)

// verboseKeyNames maps mew's internal key-token names to the beginner-facing
// spellings the keys_verbose# helper writes.
var verboseKeyNames = map[string]string{
	"esc":       "Esc",
	"escape":    "Esc",
	"space":     "Space",
	"tab":       "Tab",
	"return":    "Enter",
	"enter":     "Enter",
	"back":      "Backspace",
	"backspace": "Backspace",
	"fdel":      "Delete",
	"del":       "Delete",
	"delete":    "Delete",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"ins":       "Insert",
	"pgup":      "Page Up",
	"pgdn":      "Page Down",
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
// Modifiers spell out — ^ becomes "Ctrl+", M- "Meta+", s- "Super+", and Shift
// attaches to the base key as "Shift-" — and the keys of a chord are joined
// with "then", "followed by", and "and finally" (see joinVerboseTerms).
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
		terms[i] = verboseKeyToken(f, caseSignificant(fields, i, isBound))
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
// says whether the base letter's case encodes a real Shift (both cases bound).
func verboseKeyToken(tok string, caseSignificant bool) string {
	prefix, base := splitKeyToken(tok)
	ctrl := strings.Contains(prefix, "^")
	meta := strings.Contains(prefix, "M-")
	super := strings.Contains(prefix, "s-")
	shift := strings.Contains(prefix, "S-") // explicit Shift in the binding

	// An uppercase letter means Shift only when its case is significant — the
	// lowercase binding also exists, so the two are told apart by Shift.
	if caseSignificant && isSingleLetter(base) && base == strings.ToUpper(base) {
		shift = true
	}

	var b strings.Builder
	if ctrl {
		b.WriteString("Ctrl+")
	}
	if meta {
		b.WriteString("Meta+")
	}
	if super {
		b.WriteString("Super+")
	}
	if shift {
		b.WriteString("Shift-")
	}
	b.WriteString(verboseKeyBase(base))
	return b.String()
}

// splitKeyToken peels the modifier prefixes (^, M-, S-, s-) off a token,
// returning the accumulated prefix string and the bare base key.
func splitKeyToken(tok string) (prefix, base string) {
	base = tok
	for {
		switch {
		case strings.HasPrefix(base, "M-"):
			prefix, base = prefix+"M-", base[2:]
		case strings.HasPrefix(base, "S-"):
			prefix, base = prefix+"S-", base[2:]
		case strings.HasPrefix(base, "s-"):
			prefix, base = prefix+"s-", base[2:]
		case strings.HasPrefix(base, "^") && len(base) > 1:
			prefix, base = prefix+"^", base[1:]
		default:
			return prefix, base
		}
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
