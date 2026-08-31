//go:build mew && windows

package trinkets

import "testing"

// --pty= takes a NAME for the methods anyone has a reason to choose, so a user
// writes what they mean instead of remembering a number. The numbers all still
// work: the full table is a diagnostic surface and most of its entries exist to
// demonstrate a failure rather than to be selected.
func TestResolveMethodName(t *testing.T) {
	for name, want := range map[string]string{
		"pipe_only":  "6",
		"PIPE_ONLY":  "6", // names are case-insensitive
		" pipes ":    "6", // and surrounding space is not part of one
		"terminal":   "1",
		"conpty":     "1",
		"default":    "1",
		"inherit":    "2",
		"no_window":  "7",
		"no_console": "10",
	} {
		if got := resolveMethodName(name); got != want {
			t.Errorf("resolveMethodName(%q) = %q, want %q", name, got, want)
		}
	}
	// Anything else passes through untouched — a bare number is still the
	// canonical spelling, and an unknown name is left for optsForMethod to fall
	// back on rather than being rewritten here.
	for _, name := range []string{"", "3", "14", "nonsense"} {
		if got := resolveMethodName(name); got != name {
			t.Errorf("resolveMethodName(%q) = %q, want it unchanged", name, got)
		}
	}
}

// Every alias must name a method that exists, or --pty= would accept a spelling
// that silently falls back to the default.
func TestMethodAliasesPointAtRealMethods(t *testing.T) {
	for alias, number := range methodAliases {
		found := false
		for _, m := range ptyMethods {
			if m.Name == number {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("alias %q points at method %q, which is not in the table", alias, number)
		}
	}
}
