package config

import "strings"

const overlaySep = "\x1f"

// sectionHeader is a parsed config section name. The unified section grammar is
//
//	[ <class>:: ] <family> [ :<set> ] [ /<type> ] [ .<grammar> ]
//
// where "::" scopes a window class, ":" names a mapping set, "/" selects a
// buffer type (doc/tool/prompt), and "." selects a syntax grammar. The four
// separators are distinct, so no component is ever ambiguous with another —
// [options/tool] is a buffer type, [options.tool] would be a grammar named
// "tool". Each family reads only the components it understands; the rest stay
// empty. The header text arrives already lowercased and trimmed.
type sectionHeader struct {
	class   string
	family  string
	set     string
	bufType string
	grammar string
}

// parseSectionHeader splits a lowercased section name into its components.
// Components are peeled off their separators from the right (grammar, then
// type, then set), leaving the family; the class prefix is taken first.
func parseSectionHeader(name string) sectionHeader {
	var h sectionHeader
	rest := name
	if i := strings.Index(rest, "::"); i >= 0 {
		h.class = rest[:i]
		rest = rest[i+2:]
	}
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		h.grammar = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		h.bufType = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		h.set = rest[i+1:]
		rest = rest[:i]
	}
	h.family = rest
	return h
}

// anyMappingSet is the set name a mapping section carries when it names no set
// at all — [pty::mappings] rather than [pty::mappings:mew]. Such a section
// refines EVERY set, so a class's keymap (the isearch prompt's direction keys,
// a terminal's control bytes) does not have to be copied into each set someone
// declares. It merges just BELOW the same-scope named-set section, so a set
// that wants a different binding for one of those keys simply says so.
//
// "*" is also spellable directly ([mappings:*]) and lands in the same slot; a
// real set may not be named "*" for that reason.
const anyMappingSet = "*"

// mappingSetOf returns the set a [<class>::]mappings[:<set>] section refines,
// mapping the set-less form onto anyMappingSet. ok is false for a section that
// is not a mapping section at all.
func mappingSetOf(h sectionHeader) (string, bool) {
	if h.family != "mappings" {
		return "", false
	}
	if h.set == "" {
		return anyMappingSet, true
	}
	return h.set, true
}

// optionOverlayKey is the storage signature for an option overlay section.
func optionOverlayKey(class, grammar, bufType string) string {
	return class + overlaySep + grammar + overlaySep + bufType
}

// mappingSetKey is the storage signature for a mapping-set section.
func mappingSetKey(set, class, grammar, bufType string) string {
	return set + overlaySep + class + overlaySep + grammar + overlaySep + bufType
}

// qualCascade returns the (class, grammar, type) qualifier tuples to consult
// most-specific first: class > grammar > type. The all-empty base is excluded —
// the caller supplies it as the fallback.
func qualCascade(class, grammar, bufType string) [][3]string {
	out := make([][3]string, 0, 7)
	add := func(c, g, t string) { out = append(out, [3]string{c, g, t}) }
	if class != "" {
		if grammar != "" && bufType != "" {
			add(class, grammar, bufType)
		}
		if grammar != "" {
			add(class, grammar, "")
		}
		if bufType != "" {
			add(class, "", bufType)
		}
		add(class, "", "")
	}
	if grammar != "" && bufType != "" {
		add("", grammar, bufType)
	}
	if grammar != "" {
		add("", grammar, "")
	}
	if bufType != "" {
		add("", "", bufType)
	}
	return out
}

// optionCascade returns the overlay signatures to consult for a window with the
// given class, grammar, and buffer type, most specific first: class outranks
// grammar outranks type. The base [options] (all-empty) is excluded — the
// caller supplies it as the fallback.
func optionCascade(class, grammar, bufType string) []string {
	tuples := qualCascade(class, grammar, bufType)
	keys := make([]string, len(tuples))
	for i, t := range tuples {
		keys[i] = optionOverlayKey(t[0], t[1], t[2])
	}
	return keys
}

// ResolveOptionOverlay returns the overlaid value of option key for a window
// with the given class/grammar/type, walking the cascade most-specific first;
// found is false when no qualified section supplies it (use the base [options]).
func (c *Config) ResolveOptionOverlay(class, grammar, bufType, key string) (string, bool) {
	for _, sig := range optionCascade(class, grammar, bufType) {
		if m := c.OptionOverlays[sig]; m != nil {
			if v, ok := m[key]; ok {
				return v, true
			}
		}
	}
	return "", false
}

// HasOptionOverlay reports whether any section in the class/grammar/type
// cascade supplies option values — i.e. whether a window with this signature is
// affected by overlays at all (a plain window is not, and is left untouched).
func (c *Config) HasOptionOverlay(class, grammar, bufType string) bool {
	for _, sig := range optionCascade(class, grammar, bufType) {
		if len(c.OptionOverlays[sig]) > 0 {
			return true
		}
	}
	return false
}

// ResolveMappingSet builds the effective keymap for a set name given a window's
// class, grammar, and buffer type: the base set (the default's fully-layered
// map, or the raw [mappings:<set>] section) refined by the class/grammar/type
// cascade — [<class>::]mappings:<set>[/<type>][.<grammar>] — merged least-
// specific first so the most specific wins per key (class > grammar > type).
// defaultSet/defaultMap carry the active set's fully-resolved map (including
// built-ins) so it need not be rebuilt.
//
// At every scope the set-less section (anyMappingSet, written [<class>::mappings])
// is merged first and the named set's section second, so a set inherits the
// class's bindings without repeating them and can still override any one of
// them by naming it.
func (c *Config) ResolveMappingSet(set, class, grammar, bufType, defaultSet string, defaultMap map[string]string) (map[string]string, map[string]MappingOrigin) {
	result := make(map[string]string)
	origins := make(map[string]MappingOrigin)
	// merge overlays one layer's keymap onto the result, carrying provenance in
	// lockstep: a key set by this layer takes the layer's origin, or clears any
	// prior origin when this layer has none (a built-in override → resolve as
	// AuthorSystem when read).
	merge := func(km map[string]string, om map[string]MappingOrigin) {
		for k, v := range km {
			result[k] = v
			if o, ok := om[k]; ok {
				origins[k] = o
			} else {
				delete(origins, k)
			}
		}
	}
	mergeSig := func(sig string) { merge(c.MappingSets[sig], c.MappingSetOrigins[sig]) }
	mergeSig(mappingSetKey(anyMappingSet, "", "", ""))
	if set == defaultSet {
		merge(defaultMap, c.MappingOrigins)
	} else {
		mergeSig(mappingSetKey(set, "", "", ""))
	}
	// Merge the refinements least-specific first (reverse of the most-specific-
	// first cascade), so a more specific section overrides per key.
	tuples := qualCascade(class, grammar, bufType)
	for i := len(tuples) - 1; i >= 0; i-- {
		t := tuples[i]
		mergeSig(mappingSetKey(anyMappingSet, t[0], t[1], t[2]))
		mergeSig(mappingSetKey(set, t[0], t[1], t[2]))
	}
	return result, origins
}
