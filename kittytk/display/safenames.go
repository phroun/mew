package display

// The name a host or an app is filed under on disk.
//
// A nickname is whatever the user typed and an app name is whatever the app
// connected as: either may hold spaces, punctuation, capitals, or characters
// that no filesystem will take. What goes in a path is a cleaned form of it --
// lowercase, letters and digits and dashes -- with a number appended when the
// clean form is already taken.
//
// It is generated once and then STORED, because a directory is named by it: if
// it were recomputed from the nickname every time, changing the nickname would
// leave the old directory orphaned under a name nothing looks for again.

import (
	"strconv"
	"strings"
)

// The names used when a nickname or app name cleans away to nothing at all --
// one written in a script this cleaner keeps no letters from, or one that is
// only punctuation.
const (
	unnamedHost = "host"
	unnamedApp  = "app"
)

// cleanName is the filesystem form of a name: lowercase, with every run of
// anything else turned into a single dash and the ends trimmed. A name that
// keeps nothing comes back empty, and the caller says what to call that.
func cleanName(name string) string {
	var sb strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && sb.Len() > 0 {
				sb.WriteByte('-')
			}
			dash = false
			sb.WriteRune(r)
		default:
			dash = true
		}
	}
	return sb.String()
}

// uniqueName is base, or base with the lowest number that nobody else holds.
// The first to claim a name gets it bare: numbering something there is only one
// of reads as one of several.
func uniqueName(base string, taken map[string]bool) string {
	if base == "" {
		base = unnamedHost
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := base + "-" + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// safeName is the whole of it: clean the name, fall back if nothing survived,
// and number it if the result is spoken for.
func safeName(name, fallback string, taken map[string]bool) string {
	clean := cleanName(name)
	if clean == "" {
		clean = fallback
	}
	return uniqueName(clean, taken)
}
