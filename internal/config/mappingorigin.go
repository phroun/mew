package config

import "strings"

// Author defaults used when a mapping carries no explicit @author. Which one
// applies depends on how the mapping entered mew.
const (
	AuthorSystem     = "System"     // a built-in mapping (no config file behind it)
	AuthorCustomized = "Customized" // came from a config file that named no @author
	AuthorRemapped   = "Remapped"   // produced by a dynamic remap at runtime
)

// SourceRemap is the sentinel Source for a mapping created by a runtime remap
// (the `map` command) rather than read from a file.
const SourceRemap = "<remap>"

// MappingOrigin records where a key mapping came from: for provenance display
// ("who bound this key?") and for deterministic "last configured wins" tie-
// breaking. It travels in a map kept PARALLEL to the command keymap, so the
// plain map[string]string that the rest of the code passes around stays valid.
//
// A key sequence absent from an origins map is a built-in: treat it as
// {Author: AuthorSystem, Precedence: 0} — any config-file or remap binding
// (precedence > 0) outranks it.
type MappingOrigin struct {
	Source     string // config file / source label ("" or <remap> when not a real file)
	Line       int    // 1-based line within Source (0 when not from a file)
	Precedence int    // monotonic load-order ordinal; higher = configured later
	Author     string // @author in effect at Line, already defaulted (never "")
}

// SourcedLine is one physical config line tagged with where it came from (which
// file, which 1-based line) and the @author in effect when it was read. Include
// expansion emits these so provenance survives the flatten-into-one-stream that
// parsing consumes.
type SourcedLine struct {
	Text   string
	Source string
	Line   int
	Author string
}

// resolveAuthor returns the declared author, or dflt when the declaration was
// blank (the per-load default: AuthorCustomized for a config file, AuthorSystem
// for the built-ins).
func resolveAuthor(declared, dflt string) string {
	if a := strings.TrimSpace(declared); a != "" {
		return a
	}
	return dflt
}
