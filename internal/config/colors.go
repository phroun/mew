package config

import "strings"

// ColorScheme holds the layered, dynamically-keyed color tables loaded from
// the config file, plus built-in defaults. Colors are resolved by cascading
// through four levels, from most to least specific:
//
//  1. Window class:   [<class>::colors] section, falling back to the built-in
//     class defaults when the key is absent from the config section.
//  2. Buffer type:    [colors/<bufferType>] section, falling back to the
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
	// ByType holds [colors/<bufferType>] sections, keyed by buffer type name
	// ("doc", "tool", "prompt"). Keys within each map are lowercased.
	ByType map[string]map[string]string
	// ByClass holds [<class>::colors] sections, keyed by window class name
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
	"hint":                "\x1b[0;97;44m",   // bright white on blue (peek hints)
	"special":             "\x1b[33m",        // yellow fg - control code substitutes
	"marks":               "\x1b[0;91m",      // bright red
	"notes":               "\x1b[0;36;40m",   // cyan on black
	"linenumbers":         "\x1b[1;96;44m",   // aqua on blue
	"selection":           "\x1b[0;30;47m",   // black text on silver
	"selectioninvisibles": "\x1b[1;30;47m",   // dark gray on silver
	// Flip-safe selection: under flipBidiForHost (macOS Terminal.app etc.),
	// a background/reverse selection FILL is misplaced on any line holding
	// combining marks — the terminal's bidi engine counts codepoints where
	// the grid counts cells, so a niqqud-pointed Hebrew selection drifts and
	// half-vanishes. Foreground color and BOLD, by contrast, ride each glyph
	// through the reorder intact. So on such lines the selection is drawn as
	// bold + a distinct foreground instead of a bar: correctly positioned,
	// unambiguous (its own weight+color), and using only glyph-riding
	// channels — NO background, NO reverse, NO underline (underline drifts
	// too). Lines without combining marks (English, and Arabic, which mew
	// pre-shapes to single presentation-form codepoints) keep the real bar.
	"selectionflip":           "\x1b[0;1;93m", // bold bright-yellow (no bg)
	"selectioninvisiblesflip": "\x1b[0;1;33m", // bold yellow (no bg)
	"rulerends":           "\x1b[0;97;45m",   // bright white on magenta (end numbers)
	"rulerfill":           "\x1b[0;37;45m",   // silver on magenta (for the fill glyph)
	"rulertick":           "\x1b[0;37;45m",   // silver on magenta (for ".")
	"rulerminor":          "\x1b[0;93;45m",   // bright yellow on magenta (for ":")
	"rulermajor":          "\x1b[0;92;45m",   // bright green on magenta ("|" and numbers)
	"rulercursor":         "\x1b[0;30;47m",   // black on silver (cursor columns, rulerShowsCursor)

	// Hyperlinks (grammar-derived; see the editor's link browse mode).
	// Caret mode paints link source text in "link" ("linkrecent" is reserved
	// for recently-followed links once navigation lands); browse mode renders
	// links as buttons in the button* colors, with the *focused variants on
	// the button the caret occupies. The shadow colors paint the trailing
	// half/full-block shadow cell.
	"link":       "\x1b[0;4;93;40m", // underlined bright yellow on black
	"linkrecent": "\x1b[0;4;32;40m", // underlined green on black
	"linkhover":  "\x1b[0;4;92;40m", // underlined bright green on black (pointer over)
	// Dokuwiki headings in browse mode: a distinctive base color, non-bold so
	// browse mode can add bold/underline per level (see the editor). Bright
	// cyan on black.
	"heading": "\x1b[0;96;40m",
	// Key badges: [[keys#action|alias]] references in help text render as a
	// tight cap-less/shadow-less badge showing the live binding for the action.
	"key":                 "\x1b[0;93;45m", // bright yellow on purple
	"keyfocused":          "\x1b[0;31;47m", // red on silver (the focused badge)
	"button":              "\x1b[0;1;30;47m", // bold black on silver
	"buttonrecent":        "\x1b[0;30;47m", // black on silver (a visited link)
	"buttonshadow":        "\x1b[0;90;47m", // dark gray on silver
	"buttonshadowrecent":  "\x1b[0;34;47m", // dark blue on silver
	"buttonfocused":       "\x1b[0;30;46m", // black on cyan
	"buttonshadowfocused": "\x1b[0;90;46m", // dark gray on cyan
	"buttonpressed":       "\x1b[0;97;44m", // bright white on blue (mouse held)
	"buttonshadowpressed": "\x1b[0;37;44m", // silver on blue
	"buttonhover":         "\x1b[0;93;45m", // bright yellow on purple (pointer over)
	"buttonshadowhover":   "\x1b[0;90;45m", // dark gray on purple

	// Systematic syntax-highlighting palette. Grammar color classes map onto
	// these names (built-in conventions plus the [syntax] maps).
	"syntaxcomment":  "\x1b[0;32;40m",   // green on black
	"syntaxstring":   "\x1b[0;36;40m",   // cyan on black
	"syntaxescape":   "\x1b[0;96;40m",   // bright cyan on black
	"syntaxconstant": "\x1b[0;91;40m",   // bright red on black (numbers, literals)
	"syntaxkeyword":  "\x1b[0;1;97;40m", // bold bright white on black
	"syntaxtype":     "\x1b[0;93;40m",   // bright yellow on black
	"syntaxpreproc":  "\x1b[0;94;40m",   // bright blue on black
	"syntaxbad":      "\x1b[0;97;41m",   // bright white on red
}

// defaultTypeColors are the built-in per-buffer-type colors
// ([colors/<bufferType>] defaults).
var defaultTypeColors = map[string]map[string]string{
	"tool": {
		"text":     "\x1b[0;1;46;97m", // bright white on cyan
		"messages": "\x1b[0;1;43;97m", // bright white on amber
    	// Dokuwiki headings in browse mode: a distinctive base color, non-bold so
    	// browse mode can add bold/underline per level (see the editor). Black
    	// on cyan.
    	"heading": "\x1b[0;30;46m",
    	// Key badges: [[keys#action|alias]] references in help text render as a
    	// tight cap-less/shadow-less badge showing the live binding for the action.
    	"key":                 "\x1b[0;93;45m", // bright yellow on purple
    	"keyfocused":          "\x1b[0;31;47m", // red on silver (the focused badge)
    	"button":              "\x1b[0;1;30;47m", // black on silver
    	"buttonrecent":        "\x1b[0;30;47m", // dark red on silver (a visited link)
    	"buttonshadow":        "\x1b[0;90;47m", // dark gray on silver
    	"buttonshadowrecent":  "\x1b[0;34;47m", // dark blue on silver
    	"buttonfocused":       "\x1b[0;97;41m", // white on red
    	"buttonshadowfocused": "\x1b[0;90;41m", // dark gray on cyan
    	"buttonpressed":       "\x1b[0;97;44m", // bright white on blue (mouse held)
    	"buttonshadowpressed": "\x1b[0;37;44m", // silver on blue
    	"buttonhover":         "\x1b[0;93;45m", // bright yellow on purple (pointer over)
    	"buttonshadowhover":   "\x1b[0;90;45m", // dark gray on purple
    },
	"prompt": {
		"messages": "\x1b[0;1;42;93m", // bright yellow on green
		"text":     "\x1b[0;1;42;97m", // bright white on green
	},
}

// defaultClassColors are the built-in per-window-class colors
// ([<class>::colors] defaults).
var defaultClassColors = map[string]map[string]string{
    "quickhelp": {
        "key":         "\x1b[0;1;93;45m", // bright yellow on blue
        "text":        "\x1b[0;37;44m", // silver on blue
        "syntaxtable": "\x1b[0;35;44m", // silver on blue
    },
	"modebar": {
		"text":       "\x1b[0;44m",    // silver on blue - modebar fill
		"messages":   "\x1b[1;96;44m", // aqua on blue - stats readout (Frag/Heap/Line/Rune)
		"modifiers":  "\x1b[0;44m",    // silver on blue - active modifiers & surrounding space
		"buffer":     "\x1b[0;93;44m", // bright yellow on blue - buffer name (filename)
		"completion": "\x1b[0;44m",    // silver on blue - autocompletion & surrounding space
		"context":    "\x1b[0;92;44m", // bright green on blue - context (when autocompletion isn't showing)
		"logo":       "\x1b[1;97;41m", // bright white on red - M_ logo
	},
	"notification": {
		"messages": "\x1b[0;37;43m",
	},
	"warning": {
		// Bright yellow on brown. Use 93 (bright yellow) rather than 1;33 (bold
		// yellow): terminals that treat bold as weight-only, not "bold as bright"
		// (macOS Terminal by default), render 33 as dark yellow — brown on the
		// brown 43 background, i.e. invisible.
		"messages": "\x1b[0;93;43m",
	},
	"error": {
		"messages": "\x1b[0;97;41m", // bright white on red
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
// buffer type ("doc", "tool", "prompt"), and color name, cascading
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
