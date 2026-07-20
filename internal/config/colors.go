package config

import "strings"

// ColorScheme holds the layered, dynamically-keyed color tables loaded from
// the config file, plus built-in defaults. Colors are resolved by cascading
// through four levels, from most to least specific:
//
//  1. Window class:   [<class>.colors] section, falling back to the built-in
//     class defaults when the key is absent from the config section.
//  2. Buffer type:    [colors.<bufferType>] section, falling back to the
//     built-in buffer-type defaults when the key is absent.
//  3. Global:         [colors] section.
//  4. Global default: the built-in root defaults.
//
// At each of the first three levels: a key present with a NON-BLANK value
// wins; a key present but BLANK explicitly defers to the next level down
// (skipping that level's built-in default); a key absent from the config
// consults that level's built-in default and uses it if non-blank.
type ColorScheme struct {
	// Global holds [colors] from the config file. Keys are lowercased.
	Global map[string]string
	// ByType holds [colors.<bufferType>] sections, keyed by buffer type name
	// ("main", "work", "prompt"). Keys within each map are lowercased.
	ByType map[string]map[string]string
	// ByClass holds [<class>.colors] sections, keyed by window class name
	// (lowercased). Keys within each map are lowercased.
	ByClass map[string]map[string]string
}

// NewColorScheme returns an empty color scheme (no config overrides); all
// lookups resolve to the built-in defaults.
func NewColorScheme() ColorScheme {
	return ColorScheme{
		Global:  make(map[string]string),
		ByType:  make(map[string]map[string]string),
		ByClass: make(map[string]map[string]string),
	}
}

// defaultGlobalColors are the built-in root-level colors ([colors] defaults).
var defaultGlobalColors = map[string]string{
	"reset":               "\x1b[0m",         // reset to default
	"messages":            "\x1b[0;1;37;44m", // silver on blue
	"text":                "\x1b[0;37;40m",   // silver on black
	"invisibles":          "\x1b[0;1;40;90m", // bright black / dark gray on black
	"cursorghost":         "\x1b[0;30;100m",  // black on dark gray
	"cursoroffscreen":     "\x1b[0;30;42m",   // black on green
	"truncation":          "\x1b[0;37;41m",   // silver on red
	"hint":                "\x1b[1;37;44m",   // bright white on blue (peek hints)
	"special":             "\x1b[33m",        // yellow fg - control code substitutes
	"marks":               "\x1b[0;91m",       // bright red
	"notes":               "\x1b[0;36;40m",   // cyan on black
	"linenumbers":         "\x1b[1;96;44m",   // aqua on blue
	"selection":           "\x1b[0;30;47m",   // black text on silver
	"selectioninvisibles": "\x1b[1;30;47m",   // dark gray on silver
	"rulerends":           "\x1b[0;1;37;45m", // bright white on magenta (end numbers)
	"rulerfill":           "\x1b[0;37;45m",   // silver on magenta (for the fill glyph)
	"rulertick":           "\x1b[0;37;45m",   // silver on magenta (for ".")
	"rulerminor":          "\x1b[1;33;45m",   // bright yellow on magenta (for ":")
	"rulermajor":          "\x1b[1;32;45m",   // bright green on magenta ("|" and numbers)
	"rulercursor":         "\x1b[0;30;47m",   // black on silver (cursor columns, rulerShowsCursor)

	// Systematic syntax-highlighting palette. Grammar color classes map onto
	// these names (built-in conventions plus the [colors.syntax] maps).
	"syntaxcomment":  "\x1b[0;32;40m",   // green on black
	"syntaxstring":   "\x1b[0;36;40m",   // cyan on black
	"syntaxescape":   "\x1b[0;1;36;40m", // bright cyan on black
	"syntaxconstant": "\x1b[0;91;40m",   // bright red on black (numbers, literals)
	"syntaxkeyword":  "\x1b[0;1;97;40m", // bold bright white on black
	"syntaxtype":     "\x1b[0;93;40m",   // bright yellow on black
	"syntaxpreproc":  "\x1b[0;94;40m",   // bright blue on black
	"syntaxbad":      "\x1b[0;1;37;41m", // bright white on red
}

// defaultTypeColors are the built-in per-buffer-type colors
// ([colors.<bufferType>] defaults).
var defaultTypeColors = map[string]map[string]string{
	"work": {
		"text":     "\x1b[0;1;46;97m", // bright white on cyan
		"messages": "\x1b[0;1;43;97m", // bright white on amber
	},
	"prompt": {
		"messages": "\x1b[0;1;42;93m", // bright yellow on green
		"text":     "\x1b[0;1;42;97m", // bright white on green
	},
}

// defaultClassColors are the built-in per-window-class colors
// ([<class>.colors] defaults).
var defaultClassColors = map[string]map[string]string{
	"modebar": {
		"text":       "\x1b[0;44m",    // silver on blue - modebar fill
		"messages":   "\x1b[1;96;44m", // aqua on blue - stats readout (Frag/Heap/Line/Rune)
		"modifiers":  "\x1b[0;44m",    // silver on blue - active modifiers & surrounding space
		"buffer":     "\x1b[1;33;44m", // bright yellow on blue - buffer name (filename)
		"completion": "\x1b[0;44m",    // silver on blue - autocompletion & surrounding space
		"context":    "\x1b[1;32;44m", // bright green on blue - context (when autocompletion isn't showing)
		"logo":       "\x1b[1;97;41m", // bright white on red - M_ logo
	},
	"notification": {
		"messages": "\x1b[0;37;43m",
	},
	"warning": {
		"messages": "\x1b[1;33;43m",
	},
	"error": {
		"messages": "\x1b[1;37;41m",
	},
}

// lookupLevel checks one cascade level. It returns (color, true) when this
// level decides the color, or ("", false) when resolution should fall through
// to the next level. cfg is the level's config section (may be nil); def is
// the level's built-in defaults (may be nil).
func lookupLevel(cfg, def map[string]string, name string) (string, bool) {
	if v, ok := cfg[name]; ok {
		if v != "" {
			return v, true
		}
		// Present but blank: explicitly defer to the next level down.
		return "", false
	}
	// No config value at all: consult this level's built-in default.
	if v, ok := def[name]; ok && v != "" {
		return v, true
	}
	return "", false
}

// Resolve returns the color escape sequence for the given window class,
// buffer type ("main", "work", "prompt"), and color name, cascading
// class -> buffer type -> global -> built-in global defaults. Names, classes,
// and types are case-insensitive. Returns "" for a name unknown at every
// level.
func (cs *ColorScheme) Resolve(class, bufferType, name string) string {
	name = strings.ToLower(name)
	class = strings.ToLower(class)
	bufferType = strings.ToLower(bufferType)

	if class != "" {
		if v, ok := lookupLevel(cs.ByClass[class], defaultClassColors[class], name); ok {
			return v
		}
	}
	if bufferType != "" {
		if v, ok := lookupLevel(cs.ByType[bufferType], defaultTypeColors[bufferType], name); ok {
			return v
		}
	}
	// Global: a non-blank config value wins; missing or blank falls back to
	// the built-in global defaults.
	if v, ok := cs.Global[name]; ok && v != "" {
		return v
	}
	return defaultGlobalColors[name]
}
