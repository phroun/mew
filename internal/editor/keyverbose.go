package editor

import (
	"strings"
	"unicode"
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

// verboseKeySequence renders a space-separated key binding (e.g. "^B O") in the
// long, beginner-facing form the keys_verbose# help helper uses, for teaching
// shortcuts before the terse notation has been introduced. Modifiers are
// spelled out — ^ becomes "Ctrl+", M- "Meta+", s- "Super+", and Shift attaches
// to the base key as "Shift-" — and the keys of a chord are joined with "then",
// "followed by", and "and finally" (see joinVerboseTerms). Shift is inferred
// from letter case for NON-Ctrl keys (M-b -> Meta+B, M-B -> Meta+Shift-B); a
// Ctrl letter never carries an implied Shift (^K stays Ctrl+K, not
// Ctrl+Shift-K), only an explicit S- adds it.
func verboseKeySequence(seq string) string {
	fields := strings.Fields(seq)
	if len(fields) == 0 {
		return seq
	}
	terms := make([]string, len(fields))
	for i, f := range fields {
		terms[i] = verboseKeyToken(f)
	}
	return joinVerboseTerms(terms)
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

// verboseKeyToken renders one key token ("^B", "M-b", "S-tab", "s-x") in the
// verbose form.
func verboseKeyToken(tok string) string {
	ctrl, meta, super, shift := false, false, false, false
	base := tok
	for stripping := true; stripping; {
		switch {
		case strings.HasPrefix(base, "M-"):
			meta, base = true, base[2:]
		case strings.HasPrefix(base, "S-"):
			shift, base = true, base[2:]
		case strings.HasPrefix(base, "s-"):
			super, base = true, base[2:]
		case strings.HasPrefix(base, "^") && len(base) > 1:
			ctrl, base = true, base[1:]
		default:
			stripping = false
		}
	}

	// A Meta/Super letter written uppercase implies Shift — those combos make
	// the two letter cases distinct keys (M-b vs M-B is Meta+B vs Meta+Shift-B).
	// A Ctrl letter and a bare chord-continuation letter do NOT: mew case-folds
	// them (^C == ^c, and "^B O" == "^B o"), so their uppercase carries no
	// Shift, and only an explicit S- adds one.
	if (meta || super) && isSingleLetter(base) && base == strings.ToUpper(base) {
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
