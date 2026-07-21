package config

import "strings"

// bufferTypeNames are the window/buffer type names (window.WindowType.Name())
// used as the trailing qualifier of overlay sections, so [options.<x>] can be
// told apart: a trailing segment in this set is a buffer type, otherwise the
// segment is a grammar name.
var bufferTypeNames = map[string]bool{"doc": true, "tool": true, "prompt": true}

const overlaySep = "\x1f"

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

// parseOptionsSection classifies a (normalized, dots->underscores) section name
// as an options overlay and extracts its class, grammar, and buffer-type
// qualifiers. Forms: options | options_<quals> | <class>_options |
// <class>_options_<quals>, where <quals> is <grammar>, <type>, or
// <grammar>_<type> (a trailing buffer-type name is the type; the rest is the
// grammar). ok is false for non-options sections.
func parseOptionsSection(name string) (class, grammar, bufType string, ok bool) {
	var quals string
	switch {
	case name == "options":
		// base [options]
	case strings.HasPrefix(name, "options_"):
		quals = strings.TrimPrefix(name, "options_")
	case strings.Contains(name, "_options_"):
		i := strings.Index(name, "_options_")
		class = name[:i]
		quals = name[i+len("_options_"):]
	case strings.HasSuffix(name, "_options"):
		class = strings.TrimSuffix(name, "_options")
	default:
		return "", "", "", false
	}
	grammar, bufType = splitQuals(quals)
	return class, grammar, bufType, true
}

// parseMappingsSection classifies a section name as a mapping-set section and
// extracts its set name, class, grammar, and buffer type. Forms:
// [<class>_]mappings_<set>[_<grammar>][_<type>] — the set is the first segment
// after "mappings", a trailing buffer-type name is the type, and anything in
// between is the grammar. A set name is required.
func parseMappingsSection(name string) (set, class, grammar, bufType string, ok bool) {
	var rest string
	switch {
	case strings.HasPrefix(name, "mappings_"):
		rest = strings.TrimPrefix(name, "mappings_")
	case strings.Contains(name, "_mappings_"):
		i := strings.Index(name, "_mappings_")
		class = name[:i]
		rest = name[i+len("_mappings_"):]
	default:
		return "", "", "", "", false
	}
	if rest == "" {
		return "", "", "", "", false
	}
	parts := strings.Split(rest, "_")
	set = parts[0]
	tail := parts[1:] // [<grammar>][ _<type> ]
	if len(tail) > 0 && bufferTypeNames[tail[len(tail)-1]] {
		bufType = tail[len(tail)-1]
		tail = tail[:len(tail)-1]
	}
	grammar = strings.Join(tail, "_")
	return set, class, grammar, bufType, true
}

// splitQuals splits a qualifier tail into a leading name and an optional
// trailing buffer type. A lone buffer-type segment (e.g. "doc") is a type with
// an empty leading name, so [options.main] is a type overlay, not a grammar.
func splitQuals(quals string) (name, bufType string) {
	if quals == "" {
		return "", ""
	}
	parts := strings.Split(quals, "_")
	if bufferTypeNames[parts[len(parts)-1]] {
		return strings.Join(parts[:len(parts)-1], "_"), parts[len(parts)-1]
	}
	return quals, ""
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
// map, or the raw [mappings.<set>] section) refined by the class/grammar/type
// cascade — [<class>.]mappings.<set>[.<grammar>][.<type>] — merged least-
// specific first so the most specific wins per key (class > grammar > type).
// defaultSet/defaultMap carry the active set's fully-resolved map (including
// built-ins) so it need not be rebuilt.
func (c *Config) ResolveMappingSet(set, class, grammar, bufType, defaultSet string, defaultMap map[string]string) map[string]string {
	result := make(map[string]string)
	if set == defaultSet {
		for k, v := range defaultMap {
			result[k] = v
		}
	} else {
		for k, v := range c.MappingSets[mappingSetKey(set, "", "", "")] {
			result[k] = v
		}
	}
	// Merge the refinements least-specific first (reverse of the most-specific-
	// first cascade), so a more specific section overrides per key.
	tuples := qualCascade(class, grammar, bufType)
	for i := len(tuples) - 1; i >= 0; i-- {
		t := tuples[i]
		for k, v := range c.MappingSets[mappingSetKey(set, t[0], t[1], t[2])] {
			result[k] = v
		}
	}
	return result
}
