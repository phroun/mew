// Package config provides configuration file management for the editor.
package config

import (
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// embeddedDefaults carries mew's shipped default resource files that the
// generated editor.conf @includes — the modular key-mapping sets and the
// keyboard layouts. They are written into ~/.mew/defaults/ on first run (for
// discoverability and user editing) and also resolve the built-in mappings
// baseline without touching disk.
//
//go:embed defaults/*.conf
var embeddedDefaults embed.FS

// Config holds the editor configuration.
type Config struct {
	General    GeneralConfig
	Storage    StorageConfig
	Mappings   map[string]string
	Indicators Indicators
	Colors     ColorScheme

	// SyntaxMaps maps jsf color-class names to mew color names, so grammar
	// files plug into the systematic palette: [colors.syntax] is the global
	// map (key ""), [colors.syntax.<grammar>] overrides per grammar. Class
	// names are stored lowercased.
	SyntaxMaps map[string]map[string]string

	// OptionOverlays overlays the base [options] along three dimensions: window
	// class, syntax grammar, and buffer type. Sections are
	// [<class>.]options[.<grammar>][.<type>]; each is stored under a signature
	// (see optionOverlayKey) and resolved most-specific-first by
	// ResolveOptionOverlay with precedence class > grammar > type. syntax and
	// syntaxDetect are excluded. "default" resolves to the shipped default,
	// "inherit"/blank defer down the cascade, a real value wins. Option keys are
	// stored lowercased.
	OptionOverlays map[string]map[string]string

	// MappingSets holds the key-mapping sets refined by window class and buffer
	// type: [<class>.]mappings.<set>[.<type>] stored under a signature (see
	// mappingSetKey). ResolveMappingSet merges the class/type cascade for one
	// set name into an effective keymap. The active set name for a window comes
	// from the "mappings" option (itself overlay-resolvable).
	MappingSets map[string]map[string]string

	// Formats maps short format names / file extensions to the grammar that
	// covers them (the [formats] section), e.g. js = javascript. Built-in
	// defaults are merged first; a blank value removes an entry.
	Formats map[string]string

	// FormatPaths refines Formats by location: [formats.<ext>] sections map
	// path patterns (see PathMatches) to grammars, consulted before the
	// plain extension mapping — so a .txt inside a wiki tree can highlight
	// as dokuwiki while other .txt files stay plain. Built-in defaults are
	// merged first; a blank value removes an entry.
	FormatPaths map[string]map[string]string

	// MatchPairs holds per-grammar token pairs for go_match beyond the
	// bracket characters (the [match.<grammar>] sections): opener = closer
	// entries like if = fi, with the special entry tags = true enabling
	// HTML-style <tag></tag> matching. Openers sharing a closer count as one
	// nesting family. Built-in defaults are merged first; a blank value
	// removes an entry.
	MatchPairs map[string]map[string]string

	// Outline holds per-grammar definition patterns ([outline.<grammar>])
	// for the modebar context breadcrumb; see DefaultOutline.
	Outline map[string]map[string]string

	// ProjectDirs lists the .mew project directories whose editor.conf
	// layers were applied (outermost first) — git-style parents of the
	// working directory. Consumers also use them for project-local
	// resources (a project .mew/syntax/ joins the grammar search path).
	ProjectDirs []string
}

// Outline holds per-grammar definition patterns (the [outline.<grammar>]
// sections) used to build the modebar's context breadcrumb: each value is a
// regular expression whose LAST capture group is the definition's name. With
// two or more groups, a first group matching only whitespace or '#' sets the
// definition's nesting depth (its width; markdown heading depth is its run
// of '#'); otherwise the line's indentation is the depth. In config files,
// regex backslashes must be doubled (\\s). Built-in defaults are merged
// first; a blank value removes an entry.
//
// (declared on Config below; defaults in DefaultOutline)

// DefaultOutline is the built-in [outline.<grammar>] pattern table.
func DefaultOutline() map[string]map[string]string {
	js := map[string]string{
		"func":   `^([ \t]*)(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$]\w*)`,
		"class":  `^([ \t]*)(?:export\s+)?class\s+([A-Za-z_$]\w*)`,
		"method": `^([ \t]*)(?:async\s+)?([A-Za-z_$]\w*)\s*\([^)]*\)\s*\{\s*$`,
	}
	javaish := map[string]string{
		"class":  `^([ \t]*)(?:\w+\s+)*class\s+(\w+)`,
		"method": `^([ \t]*)(?:[\w<>\[\],.]+\s+)+(\w+)\s*\([^;]*\)\s*\{`,
	}
	return map[string]map[string]string{
		"go": {
			"func": `^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`,
			"type": `^type\s+([A-Za-z_]\w*)`,
		},
		"python": {
			"def":   `^([ \t]*)(?:async[ \t]+)?def\s+(\w+)`,
			"class": `^([ \t]*)class\s+(\w+)`,
		},
		"javascript": js,
		"typescript": js,
		"java":       javaish,
		"csharp":     javaish,
		"shell": {
			"func":   `^(?:function\s+)?([A-Za-z_]\w*)\s*\(\)`,
			"funckw": `^function\s+([A-Za-z_]\w*)`,
		},
		"lua": {
			"func": `^([ \t]*)(?:local\s+)?function\s+([\w.:]+)`,
		},
		"rust": {
			"fn":   `^([ \t]*)(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`,
			"impl": `^([ \t]*)impl(?:\s*<[^>]*>)?\s+(?:[\w:]+\s+for\s+)?([A-Za-z_]\w*)`,
		},
		"cpp": {
			"func": `^[A-Za-z_][\w:<>~&*\s]*?\b([A-Za-z_~]\w*)\s*\([^;]*$`,
		},
		"php": {
			"func":  `^([ \t]*)(?:\w+\s+)*function\s+(\w+)`,
			"class": `^([ \t]*)(?:\w+\s+)*class\s+(\w+)`,
		},
		"elisp": {
			"def": `^\(def\w*\s+'?([\w-]+)`,
		},
		"scheme": {
			"def": `^\(define(?:-\w+)?\s+[('` + "`" + `]*([\w!?<>=*/+-]+)`,
		},
		"markdown": {
			"heading": `^(#+)[ \t]+(.+?)[ \t]*$`,
		},
		"dokuwiki": {
			"heading": `^(={2,6})[ \t]*([^=].*?)[ \t]*=*[ \t]*$`,
		},
		"tex": {
			"chapter": `^\\chapter\*?\{([^}]*)\}`,
			"section": `^\\(?:sub)*section\*?\{([^}]*)\}`,
		},
		"pawscript": {
			"macro": `^([ \t]*)macro\s+(\w+)`,
		},
		"make": {
			"target": `^([A-Za-z_][\w./-]*)\s*:`,
		},
		"yaml": {
			"key": `^([ \t]*)([\w-]+)\s*:`,
		},
		"conf": {
			"section": `^\[([^\]]+)\]`,
		},
		"toml": {
			"section": `^\[+([^\]]+?)\]+\s*$`,
		},
	}
}

// DefaultMatchPairs is the built-in [match.<grammar>] table.
func DefaultMatchPairs() map[string]map[string]string {
	return map[string]map[string]string{
		"shell": {"if": "fi", "case": "esac", "do": "done"},
		"lua":   {"if": "end", "do": "end", "function": "end", "repeat": "until"},
		"make": {
			"ifeq": "endif", "ifneq": "endif", "ifdef": "endif",
			"ifndef": "endif", "define": "endef",
		},
		"cpp":  {"#if": "#endif", "#ifdef": "#endif", "#ifndef": "#endif"},
		"tex":  {"\\begin": "\\end"},
		"html": {"tags": "true"},
		"php":  {"tags": "true"},
	}
}

// DefaultFormats is the built-in [formats] table: short format names and
// file extensions mapped to the grammar file that covers them. The user's
// [formats] section merges over these (a blank value removes an entry).
func DefaultFormats() map[string]string {
	return map[string]string{
		"c": "cpp", "h": "cpp", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "inc": "cpp",
		"js": "javascript", "mjs": "javascript", "cjs": "javascript",
		"jsx": "javascript", "es": "javascript", "es6": "javascript", "json": "javascript",
		"ts": "typescript", "tsx": "typescript",
		"py": "python", "pyw": "python", "rpy": "python",
		"sh": "shell", "bash": "shell", "ksh": "shell", "zsh": "shell",
		"dash": "shell", "ash": "shell",
		"node": "javascript", "nodejs": "javascript",
		"md":  "markdown",
		"ini": "conf", "htaccess": "conf",
		"yml": "yaml",
		"el":  "elisp", "emacs": "elisp",
		"scm": "scheme", "ss": "scheme", "sld": "scheme", "sls": "scheme",
		"sps": "scheme", "sc": "scheme",
		"makefile": "make", "mk": "make",
		"rs":  "rust",
		"psl": "pawscript", "paw": "pawscript", "mew": "pawscript",
		"cs":  "csharp",
		"htm": "html", "xhtml": "html", "svg": "html", "xml": "html",
		"phtml": "php",
		"ltx":   "tex", "latex": "tex",
		"golang": "go",
		"wiki":   "dokuwiki",
		// emacs mode names (modeline detection).
		"c++": "cpp", "shell-script": "shell", "emacs-lisp": "elisp",
		"latex-mode": "tex",
	}
}

// DefaultFormatPaths is the built-in [formats.<ext>] table: path-conditional
// refinements of the extension mapping. DokuWiki stores its pages as .txt,
// so a .txt whose path mentions wiki — or that lives under a folder named
// pages (DokuWiki's data/pages tree) — highlights as dokuwiki.
func DefaultFormatPaths() map[string]map[string]string {
	return map[string]map[string]string{
		"txt": {
			"*wiki*": "dokuwiki",
			"pages":  "dokuwiki",
		},
	}
}

// Indicators holds the glyphs/labels used to draw editor chrome (ruler ticks,
// whitespace markers, gutter/cursor indicators, peek tab labels). Configured via
// the [indicators] section.
type Indicators struct {
	RulerFill       string
	RulerTick       string
	RulerMinor      string
	RulerMajor      string
	VisibleSpace    string
	VisibleTabStart string
	VisibleTabFill  string
	VisibleTabEnd   string
	VisibleNewline  string
	VisibleReturn   string
	GutterEmpty     string
	CursorGhost     string
	CursorOffScreen string
	TruncationLeft  string
	TruncationRight string
	StatPeekUp      string
	StatPeekDown    string
	PromptPeekUp    string
	PromptPeekDown  string
	// Link-as-button chrome (browse mode): a button renders as
	// ButtonLeft + title + ButtonRight + ButtonShadow, with the Focused*
	// variants when the caret is inside the button.
	ButtonLeft          string
	ButtonRight         string
	ButtonShadow        string
	FocusedButtonLeft   string
	FocusedButtonRight  string
	FocusedButtonShadow string
}

// DefaultIndicators returns the built-in indicator glyphs.
func DefaultIndicators() Indicators {
	return Indicators{
		RulerFill:       "░",
		RulerTick:       ".",
		RulerMinor:      ":",
		RulerMajor:      "|",
		VisibleSpace:    "·",
		VisibleTabStart: ":",
		VisibleTabFill:  "-",
		VisibleTabEnd:   ">|",
		VisibleNewline:  "⮐",
		VisibleReturn:   "M",
		GutterEmpty:     "~",
		CursorGhost:     "|",
		CursorOffScreen: "@",
		TruncationLeft:  "<",
		TruncationRight: ">",
		StatPeekUp:      "[%SPU%]",
		StatPeekDown:    "[%SPD%]",
		PromptPeekUp:    "[%PPU%]",
		PromptPeekDown:  "[%PPD%]",

		ButtonLeft:          " ",
		ButtonRight:         " ",
		ButtonShadow:        "▐",
		FocusedButtonLeft:   "<",
		FocusedButtonRight:  ">",
		FocusedButtonShadow: "█",
	}
}

// GeneralConfig holds general editor settings.
type GeneralConfig struct {
	ShowLineNumbers  bool
	ShowColumnRuler  bool
	RulerShowsCursor bool
	TabSize          int
	ShowInvisibles   bool
	ShowBidi         bool
	ShowMarks        string // "no" | "yes" | "all"
	// OverwriteMode is the inverse of the user-facing insertMode option: false
	// (the zero value, and the default) means insert mode; true means typing
	// overwrites the character under the caret (except at end of line, where it
	// appends). Stored inverted so the zero value is the intended default.
	OverwriteMode bool
	// ReadOnly rejects content-mutating edits made through a window (typing,
	// deleting, replacing, pasting). Navigation, search, marks, and undo/redo
	// still work. Per window; false by default.
	ReadOnly bool

	// LinkBrowsing enables the hyperlink layer for grammar-recognized links
	// (link coloring, browse-mode buttons, arming on caret entry). Off, links
	// render exactly as their grammar colors them, with no interaction. Per
	// window; on by default.
	LinkBrowsing bool

	// Syntax names the syntax-highlighting grammar (a jsf file, looked up on
	// the syntax search path). Empty disables highlighting.
	Syntax string

	// SyntaxDetect auto-detects each buffer's grammar: a #! shebang on the
	// first line wins, else the buffer's file extension (or extensionless
	// basename), both resolved through the [formats] table. Buffers that
	// detect nothing fall back to the syntax option.
	SyntaxDetect bool

	// StartPath, when set, is where a session STARTS when its working
	// directory cannot be derived any other way: GUI launches typically begin
	// at the filesystem root, and the editor then falls back to the last main
	// buffer's file location, this path, then the user's home (see the
	// editor's start-directory resolution).
	StartPath string

	// SyntaxOverrides is a space-separated list of grammar flavors (e.g.
	// "go conf") whose highlighter should ignore the document's own project
	// .mew/syntax folder and resolve from the user's copy (mew:/syntax), the
	// built-in set, or JOE instead. A per-window option: it descends through
	// the option overlay and can be overridden for one window or buffer.
	SyntaxOverrides string

	// The matchIgnores* flags configure go_match's FALLBACK context scanner
	// for buffers with no syntax grammar: enabled constructs read as
	// string/comment regions, so their brackets and tokens neither answer
	// nor start matches outside them (and quote mates work). When a grammar
	// applies, the real highlighter context supersedes all of these —
	// mew's equivalent of JOE's always-on highlighter_context. Quotes do
	// not span lines; SlashStar does. Defaults: double quotes on (strings
	// are near-universal), everything else off — single quotes would
	// misread prose apostrophes, and the comment markers are language-
	// specific.
	MatchIgnoresSingleQuote  bool // '...'
	MatchIgnoresDoubleQuote  bool // "..."
	MatchIgnoresSlashStar    bool // /* ... */ (multi-line)
	MatchIgnoresSlashSlash   bool // // to end of line
	MatchIgnoresHash         bool // # to end of line
	MatchIgnoresDoubleHyphen bool // -- to end of line
	MatchIgnoresSemicolon    bool // ; to end of line
	MatchIgnoresPercent      bool // % to end of line

	// MacOptionKeys controls the macOS Option-key layer: "auto" (default)
	// decodes Option characters into M- keys on macOS only; "true" forces
	// the decode everywhere; "false" disables it. Whenever the layer is not
	// "false", unmapped M- keys re-insert the Option character — so
	// bindings steal individual combos while everything else types
	// seamlessly, and Alt on any platform gains the mac character layer.
	MacOptionKeys string

	// UseLocks enables editing locks entirely: emacs-interoperable lock
	// files next to the source when possible, mew-native locks in the
	// nearest .mew directory otherwise. false turns all locking off.
	UseLocks bool

	// UseEmacsLocks enables the emacs-interoperable ".#<name>" lock files
	// specifically (only consulted while UseLocks is true). The planned
	// prose personality sets this false so manuscript folders never
	// collect lock symlinks; mew-native locks still apply.
	UseEmacsLocks bool

	// ProjectConfig enables the git-style project cascade: .mew directories
	// found walking up from the working directory contribute their
	// editor.conf as layers over the user configuration (set it false in
	// ~/.mew/editor.conf to ignore project configs entirely).
	ProjectConfig    bool
	WordWrap         bool
	Layout           string
	MappingsName     string
	SequenceLength   int
	DebounceMs       int
	MaxRenderDelayMs int

	// Search defaults (JOE-compatible rc options): SearchIgnoreCase mirrors
	// -icase, SearchWrap mirrors -wrap, SearchRegex mirrors -regex (standard
	// regular-expression syntax by default instead of the JOE syntax).
	SearchIgnoreCase bool
	SearchWrap       bool
	SearchRegex      bool

	// ModebarLocation places the modebar on the "top" (default) or
	// "bottom" screen line.
	ModebarLocation string

	// Modebar templates: text with %CODE% substitutions (%% = a literal %).
	// Inner is the buffer/filename region, Default is the middle text shown
	// when no live context (key-sequence completion or outline breadcrumb)
	// applies, Outer is the right-hand readout (ellipsized when too long).
	// Codes: %FN% %FORTUNE% %FRAG% %HEAP% %LINE% %RUNE% %COL% %LINEBYTE%
	// %ABSBYTE% (more later).
	ModebarInner   string
	ModebarDefault string
	ModebarOuter   string

	// PromptTimeout is how long (in seconds) a prompt-suspended command
	// sequence stays resumable; answering the prompt after that fails
	// safe. ScriptTimeout bounds other host-level async script tokens.
	// 0 means never time out; unset defaults to 300 (5 minutes).
	PromptTimeout int
	ScriptTimeout int

	// Paging (go_page_prior/go_page_next):
	//   PageSizeOptimal    - desired distance: a fixed count ("24") or a
	//                        percentage of the view height ("50%"). Default "100%".
	//   PageOverlapMinimum - context to keep between pages: a fixed count ("2")
	//                        or a percentage of the view height ("10%"). The
	//                        page never exceeds height-overlap (floored so a
	//                        tiny window still moves >=1). A percentage is
	//                        rounded up so a non-zero percent never vanishes;
	//                        only "0" gives no overlap. Default "1".
	//   PageSizeStep       - round the distance down to a multiple of this
	//                        (e.g. 6 snaps to 12 or 24). 0 = no rounding.
	PageSizeOptimal    string
	PageOverlapMinimum string
	PageSizeStep       int

	// MaxRepeat bounds the count repeat_next will honor, so a mistyped or
	// pasted huge value can't lock the editor running one command a runaway
	// number of times. A larger requested count is clamped to this. Default 100.
	MaxRepeat int

	// KillRingEntries is how many kill-ring entries are retained; pushing past
	// it evicts the oldest. Default 10.
	KillRingEntries int

	// Direction is the base text direction every line begins in: "ltr"
	// (default) or "rtl". RTL segments within a line are resolved and
	// rendered bidirectionally either way; this sets the line's base
	// direction for the bidi algorithm.
	Direction string

	// FlipBidiForHost re-emits RTL runs in logical order for host terminals
	// that apply their own bidi reordering (macOS Terminal.app): "true" flips,
	// "false" emits mew's computed visual order (correct for stream-order
	// terminals: iTerm2, xterm, most others), and "auto" (the default) probes
	// the terminal once, on the first frame that contains RTL content, and
	// decides from its answer (falling back to the TERM_PROGRAM environment).
	FlipBidiForHost string
}

// StorageConfig holds local storage locations ([storage] section). These are
// honored only when the configuration comes from the local config file - a
// host-supplied config string cannot redirect where scratch data lands.
type StorageConfig struct {
	// Scratch is the local directory handed to Garland for cold storage.
	// Empty means the system temp directory.
	Scratch string

	// Backups is the directory Garland streams automatic pre-session
	// backups into. Empty means ~/.mew/backups. A relative path in a
	// project layer resolves inside that project's .mew folder.
	Backups string

	// Deadcat overrides where the crash/kill dump of modified buffers is
	// written (the DEADCAT file — mew's DEADJOE). Empty means the directory
	// editor.conf loaded from (normally ~/.mew). A relative path in a project
	// layer resolves inside that project's .mew folder.
	Deadcat string

	// Documents is the fallback directory filename completion globs when a
	// buffer has no file of its own to anchor to (a fresh buffer). Empty
	// falls back to the launch directory (standalone) or the host's default
	// (module mode).
	Documents string
}

// FileIO is the file access the Manager uses for config, profile, and @include
// files. A path beginning "mew:" addresses mew's own config tree (editor.conf,
// profile.mew, angle-bracket includes); every other path is an ordinary
// project/document file. Paths handed to a FileIO always use the canonical
// "mew:///rel" spelling (empty authority — the authority slot is reserved for
// future instance selection). A host injects a scheme-aware implementation
// (SetFileIO); the default maps mew:/// to <UserHomeDir>/.mew and everything
// else straight to the OS. Write creates parent directories.
type FileIO struct {
	Read  func(path string) ([]byte, error)
	Write func(path string, data []byte) error
	IsDir func(path string) bool
}

// Manager handles configuration file operations.
type Manager struct {
	configDir  string // "mew:///" — the mew config tree root
	configPath string // "mew:///editor.conf"
	fio        FileIO
	// localMewDir, when set, is the real ~/.mew directory: excluded from
	// project discovery so the user config layer is not also treated as a
	// project layer when home is an ancestor of the working directory.
	localMewDir string

	// includeRead, when set (legacy SetIncludeReader), overrides @include reads
	// specifically; nil routes them through fio like everything else.
	includeRead func(path string) ([]byte, error)
}

// NewManager creates a new config manager whose config tree is addressed with
// the "mew:///" scheme (default: <UserHomeDir>/.mew on the real OS).
func NewManager() *Manager {
	return &Manager{
		configDir:  "mew:///",
		configPath: "mew:///editor.conf",
		fio:        osFileIO(),
	}
}

// SetFileIO substitutes the Manager's file access (see FileIO).
func (m *Manager) SetFileIO(io FileIO) { m.fio = io }

// io returns the Manager's file access, defaulting to osFileIO when the
// Manager was built without one (a bare &Manager{}, as some tests do).
func (m *Manager) io() FileIO {
	if m.fio.Read == nil {
		m.fio = osFileIO()
	}
	return m.fio
}

// SetLocalMewDir names the real ~/.mew directory to exclude from project
// discovery (local mode only).
func (m *Manager) SetLocalMewDir(dir string) { m.localMewDir = dir }

// SetIncludeReader (legacy) routes only @include reads through a host reader.
// Prefer SetFileIO. Retained for compatibility.
func (m *Manager) SetIncludeReader(read func(path string) ([]byte, error)) {
	m.includeRead = read
}

// osFileIO is the default file access: mew:/// maps to <UserHomeDir>/.mew,
// other paths go straight to the OS.
func osFileIO() FileIO {
	return FileIO{
		Read: func(p string) ([]byte, error) { return os.ReadFile(mewToLocal(p)) },
		Write: func(p string, data []byte) error {
			lp := mewToLocal(p)
			if err := os.MkdirAll(filepath.Dir(lp), 0o755); err != nil {
				return err
			}
			return os.WriteFile(lp, data, 0o644)
		},
		IsDir: func(p string) bool {
			fi, err := os.Stat(mewToLocal(p))
			return err == nil && fi.IsDir()
		},
	}
}

// mewToLocal maps a "mew:///rel" path to <UserHomeDir>/.mew/rel; other paths
// pass through unchanged.
func mewToLocal(p string) string {
	if !strings.HasPrefix(p, "mew:") {
		return p
	}
	rel := strings.TrimLeft(strings.TrimPrefix(p, "mew:"), "/")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".mew", filepath.FromSlash(rel))
}

func (m *Manager) readInclude(path string) ([]byte, error) {
	if m.includeRead != nil {
		return m.includeRead(path)
	}
	return m.io().Read(path)
}

// includeRe matches the "@include" directive — an at-rule (à la CSS/Sass),
// visually distinct from "#" comments and carrying its own syntax-highlight
// class. Quotes resolve relative to the including file (through the same file
// system it came from); angle brackets resolve in the standard mew config
// directory.
var includeRe = regexp.MustCompile(`^\s*@include\s+(?:"([^"]+)"|<([^>]+)>)\s*$`)

// joinInclude resolves an @include reference against a base directory. When the
// base is a "mew:" path the result stays inside the mew tree: the reference is
// joined as if rooted at the tree top, so any leading "../" is dropped and can
// never rise above "mew:///" — the confinement a virtualizing host relies on.
// Other bases join with ordinary OS-path semantics.
func joinInclude(base, ref string) string {
	if strings.HasPrefix(base, "mew:") {
		rel := strings.TrimLeft(strings.TrimPrefix(base, "mew:"), "/")
		clean := path.Clean("/" + rel + "/" + ref) // rooted: ".." cannot escape "/"
		return "mew://" + clean
	}
	return filepath.Join(base, ref)
}

// includeDir is the directory of an include path, preserving the "mew:///"
// scheme so a nested quoted include resolves against its includer's mew
// location.
func includeDir(p string) string {
	if strings.HasPrefix(p, "mew:") {
		rel := strings.TrimLeft(strings.TrimPrefix(p, "mew:"), "/")
		return "mew://" + path.Dir("/"+rel)
	}
	return filepath.Dir(p)
}

// expandIncludes splices @include directives into the content, recursively.
// Each file is included at most once (repeats and cycles are dropped), and
// nesting is bounded. A failed include becomes a comment noting the failure
// so the rest of the configuration still parses.
func (m *Manager) expandIncludes(content, base string, depth int, visited map[string]bool) string {
	if depth > 16 {
		return content
	}
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		sub := includeRe.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if sub == nil {
			out = append(out, line)
			continue
		}
		var incPath, nextBase string
		if sub[1] != "" {
			// "quoted": relative to the including source.
			incPath = joinInclude(base, sub[1])
			nextBase = includeDir(incPath)
		} else {
			// <angle>: the standard mew location.
			incPath = joinInclude(m.configDir, sub[2])
			nextBase = m.configDir
		}
		if visited[incPath] {
			continue
		}
		visited[incPath] = true
		data, err := m.readInclude(incPath)
		if err != nil {
			out = append(out, "# @include failed: "+incPath)
			continue
		}
		out = append(out, m.expandIncludes(string(data), nextBase, depth+1, visited))
	}
	return strings.Join(out, "\n")
}

// DefaultConfig returns sensible default configuration, including the
// built-in key mappings (so sessions that never read the config file — a
// host-supplied config string, SkipUserConfig — still have working keys).
func DefaultConfig() Config {
	return Config{
		Formats:     DefaultFormats(),
		FormatPaths: DefaultFormatPaths(),
		MatchPairs:  DefaultMatchPairs(),
		Outline:     DefaultOutline(),
		General: GeneralConfig{
			ShowLineNumbers:         true,
			ShowColumnRuler:         true,
			RulerShowsCursor:        false,
			TabSize:                 4,
			ShowInvisibles:          false,
			ShowBidi:                false,
			ShowMarks:               "no",
			OverwriteMode:           false, // insertMode=yes
			ReadOnly:                false,
			LinkBrowsing:            true,
			ProjectConfig:           true,
			UseLocks:                true,
			UseEmacsLocks:           true,
			MatchIgnoresDoubleQuote: true,
			MacOptionKeys:           "auto",
			WordWrap:                false,
			Layout:                  "qwerty",
			MappingsName:            "mew",
			SequenceLength:          4,
			DebounceMs:              20,
			MaxRenderDelayMs:        100,
			SearchIgnoreCase:        false,
			SearchWrap:              true,
			SearchRegex:             false,
			ModebarLocation:         "top",
			ModebarInner:            "%FN%",
			ModebarDefault:          "%FORTUNE%",
			ModebarOuter:            "Frag:%FRAG% Heap:%HEAP% Line:%LINE% Rune:%RUNE%",
			PromptTimeout:           300,
			ScriptTimeout:           300,
			PageSizeOptimal:         "100%",
			PageOverlapMinimum:      "1",
			PageSizeStep:            0,
			MaxRepeat:               100,
			KillRingEntries:         10,
			Direction:               "ltr",
			FlipBidiForHost:         "auto",
		},
		Mappings:   builtinMappings(),
		Indicators: DefaultIndicators(),
		Colors:     NewColorScheme(),
	}
}

// builtinMappings returns a fresh copy of the default key mappings, parsed
// once from the built-in config text.
func builtinMappings() map[string]string {
	builtinMappingsOnce.Do(func() {
		m := &Manager{}
		// The default config @includes the modular keymap files; expand them from
		// the embedded copies so the baseline is complete without disk access.
		parsed := m.parseConfigFile(expandEmbeddedIncludes(m.generateDefaultConfig()))
		builtinMappingsCache = parsed["mappings_mew"]
		if builtinMappingsCache == nil {
			builtinMappingsCache = map[string]string{}
		}
	})
	out := make(map[string]string, len(builtinMappingsCache))
	for k, v := range builtinMappingsCache {
		out[k] = v
	}
	return out
}

var (
	builtinMappingsOnce  sync.Once
	builtinMappingsCache map[string]string
)

// Load loads configuration from file.
func (m *Manager) Load() (Config, error) {
	config := DefaultConfig()

	// Read the user config; create the default when it is missing.
	content, err := m.io().Read(m.configPath)
	if err != nil {
		if werr := m.WriteDefault(); werr != nil {
			return config, fmt.Errorf("failed to create default config: %w", werr)
		}
		content = []byte(m.generateDefaultConfig())
	}

	// The user layer: quoted @include directives resolve relative to the
	// config file's own directory (the mew: tree root).
	m.applyLayer(&config, string(content), m.configDir, false)

	// Project layers: git-style .mew directories walking up from the
	// working directory, outermost first, each editor.conf building on the
	// layers above it. The nearest project wins last. profile.mew is NEVER
	// loaded from project directories (a cloned repository must not run
	// scripts); project syntax/ directories join the grammar search path
	// via Config.ProjectDirs.
	if config.General.ProjectConfig {
		if cwd, err := os.Getwd(); err == nil {
			for _, mewDir := range m.projectMewDirs(cwd) {
				config.ProjectDirs = append(config.ProjectDirs, mewDir)
				if src, err := m.io().Read(filepath.Join(mewDir, "editor.conf")); err == nil {
					m.applyLayer(&config, string(src), mewDir, true)
				}
			}
		}
	}
	return config, nil
}

// projectMewDirs walks from start up to the filesystem root and returns the
// .mew project directories along the way, OUTERMOST first, through the
// Manager's FileIO (so a virtualized host's document tree participates). The
// real ~/.mew (localMewDir) is excluded — it is the user config layer, not a
// project.
func (m *Manager) projectMewDirs(start string) []string {
	var chain []string
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil
	}
	exclude := m.localMewDir
	if exclude != "" {
		if a, err := filepath.Abs(exclude); err == nil {
			exclude = a
		}
	}
	for {
		mew := filepath.Join(dir, ".mew")
		if mew != exclude && m.io().IsDir(mew) {
			chain = append(chain, mew)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// LoadFromString parses configuration from a string (a host-supplied
// editor.conf equivalent). @include directives are honored: quoted paths are
// requested as-is (relative, through the include reader — the host's file
// system when one is set), angle-bracket paths under the standard mew
// directory.
func (m *Manager) LoadFromString(content string) Config {
	return m.loadExpanded(content, "")
}

func (m *Manager) loadExpanded(content, base string) Config {
	config := DefaultConfig()
	m.applyLayer(&config, content, base, false)
	return config
}

// applyLayer parses one configuration source OVER an existing Config: values
// present in the source override; everything else is inherited from the
// layers already applied. This is the unit of the config cascade — built-in
// defaults, then ~/.mew/editor.conf, then each project .mew/editor.conf from
// the outermost tree down to the nearest. base resolves quoted @include
// directives and relative [storage] paths. project marks a project layer:
// its key mappings merge per key instead of replacing the map (a project
// adds bindings; it does not silently discard the user's keymap).
func (m *Manager) applyLayer(config *Config, content, base string, project bool) {
	// Splice @include directives, then parse.
	content = m.expandIncludes(content, base, 0, map[string]bool{})
	parsed := m.parseConfigFile(content)

	// Value keywords for the scalar sections: "default" restores the shipped
	// value, "inherit"/blank fall through to earlier layers (blank keeps its
	// meaning where an empty value is itself a setting).
	if opt, ok := parsed["options"]; ok {
		normalizeScalarSection(opt, defaultSectionValues("options"),
			map[string]bool{"syntax": true})
	}
	if general, ok := parsed["general"]; ok {
		normalizeScalarSection(general, defaultSectionValues("general"), nil)
	}
	if ind, ok := parsed["indicators"]; ok {
		normalizeScalarSection(ind, defaultSectionValues("indicators"), nil)
	}
	if storage, ok := parsed["storage"]; ok {
		for k, v := range storage {
			if keywordOf(v) != "" {
				storage[k] = "" // default/inherit: the system temp location
			}
		}
	}

	// Apply [options]: the runtime-settable editor options (the set_option
	// surface, and the command-line --flags). These are keyed by name in
	// setOption, so the config section they live in is purely organizational;
	// they moved here from [general] to make the "settable option" vs
	// "structural setting" division explicit.
	if opt, ok := parsed["options"]; ok {
		if v, ok := opt["showLineNumbers"]; ok {
			config.General.ShowLineNumbers = parseBool(v, true)
		}
		if v, ok := opt["showColumnRuler"]; ok {
			config.General.ShowColumnRuler = parseBool(v, true)
		}
		if v, ok := opt["rulerShowsCursor"]; ok {
			config.General.RulerShowsCursor = parseBool(v, false)
		}
		if v, ok := opt["tabSize"]; ok {
			config.General.TabSize = parseInt(v, 4)
		}
		if v, ok := opt["showInvisibles"]; ok {
			config.General.ShowInvisibles = parseBool(v, false)
		}
		if v, ok := opt["showBidi"]; ok {
			config.General.ShowBidi = parseBool(v, false)
		}
		if v, ok := opt["showMarks"]; ok {
			if s, valid := ParseShowMarks(v); valid {
				config.General.ShowMarks = s
			}
		}
		if v, ok := opt["insertMode"]; ok {
			// Stored inverted: insertMode=yes -> not overwrite.
			config.General.OverwriteMode = !parseBool(v, true)
		}
		if v, ok := opt["readOnly"]; ok {
			config.General.ReadOnly = parseBool(v, false)
		}
		if v, ok := opt["linkBrowsing"]; ok {
			config.General.LinkBrowsing = parseBool(v, true)
		}
		if v, ok := opt["syntax"]; ok {
			v = stripQuotes(strings.TrimSpace(v))
			if strings.EqualFold(v, "none") {
				v = ""
			}
			config.General.Syntax = v
		}
		if v, ok := opt["syntaxDetect"]; ok {
			config.General.SyntaxDetect = parseBool(v, false)
		}
		if v, ok := opt["syntaxOverrides"]; ok {
			config.General.SyntaxOverrides = stripQuotes(strings.TrimSpace(v))
		}
		if v, ok := opt["macOptionKeys"]; ok {
			switch strings.ToLower(stripQuotes(strings.TrimSpace(v))) {
			case "auto", "true", "false":
				config.General.MacOptionKeys = strings.ToLower(stripQuotes(strings.TrimSpace(v)))
			}
		}
		matchFlags := map[string]*bool{
			"matchIgnoresSingleQuote":  &config.General.MatchIgnoresSingleQuote,
			"matchIgnoresDoubleQuote":  &config.General.MatchIgnoresDoubleQuote,
			"matchIgnoresSlashStar":    &config.General.MatchIgnoresSlashStar,
			"matchIgnoresSlashSlash":   &config.General.MatchIgnoresSlashSlash,
			"matchIgnoresHash":         &config.General.MatchIgnoresHash,
			"matchIgnoresDoubleHyphen": &config.General.MatchIgnoresDoubleHyphen,
			"matchIgnoresSemicolon":    &config.General.MatchIgnoresSemicolon,
			"matchIgnoresPercent":      &config.General.MatchIgnoresPercent,
		}
		for key, dst := range matchFlags {
			if v, ok := opt[key]; ok {
				*dst = parseBool(v, *dst)
			}
		}
		if v, ok := opt["wordWrap"]; ok {
			config.General.WordWrap = parseBool(v, false)
		}
		if v, ok := opt["searchIgnoreCase"]; ok {
			config.General.SearchIgnoreCase = parseBool(v, false)
		}
		if v, ok := opt["searchWrap"]; ok {
			config.General.SearchWrap = parseBool(v, true)
		}
		if v, ok := opt["searchRegex"]; ok {
			config.General.SearchRegex = parseBool(v, false)
		}
		if v, ok := opt["modebarLocation"]; ok {
			if loc := strings.ToLower(strings.TrimSpace(v)); loc == "top" || loc == "bottom" {
				config.General.ModebarLocation = loc
			}
		}
		if v, ok := opt["modebarInner"]; ok {
			config.General.ModebarInner = stripQuotes(v)
		}
		if v, ok := opt["modebarDefault"]; ok {
			config.General.ModebarDefault = stripQuotes(v)
		}
		if v, ok := opt["modebarOuter"]; ok {
			config.General.ModebarOuter = stripQuotes(v)
		}
		if v, ok := opt["promptTimeout"]; ok {
			if n := parseInt(v, 300); n >= 0 {
				config.General.PromptTimeout = n
			}
		}
		if v, ok := opt["scriptTimeout"]; ok {
			if n := parseInt(v, 300); n >= 0 {
				config.General.ScriptTimeout = n
			}
		}
		if v, ok := opt["pageSizeOptimal"]; ok {
			if s := strings.TrimSpace(v); s != "" {
				config.General.PageSizeOptimal = s
			}
		}
		if v, ok := opt["pageOverlapMinimum"]; ok {
			if s := strings.TrimSpace(v); s != "" {
				config.General.PageOverlapMinimum = s
			}
		}
		if v, ok := opt["pageSizeStep"]; ok {
			if n := parseInt(v, 0); n >= 0 {
				config.General.PageSizeStep = n
			}
		}
		if v, ok := opt["maxRepeat"]; ok {
			if n := parseInt(v, 100); n >= 1 {
				config.General.MaxRepeat = n
			}
		}
		if v, ok := opt["killRingEntries"]; ok {
			if n := parseInt(v, 10); n >= 1 {
				config.General.KillRingEntries = n
			}
		}
		if v, ok := opt["direction"]; ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "ltr":
				config.General.Direction = "ltr"
			case "rtl":
				config.General.Direction = "rtl"
			}
		}
		if v, ok := opt["flipBidiForHost"]; ok {
			switch strings.ToLower(stripQuotes(strings.TrimSpace(v))) {
			case "auto", "true", "false":
				config.General.FlipBidiForHost = strings.ToLower(stripQuotes(strings.TrimSpace(v)))
			}
		}
	}

	// Apply [general]: structural / load-time settings that are not runtime
	// set_option options (key layout and mappings, the project cascade toggle,
	// editing locks, key-sequence length).
	if general, ok := parsed["general"]; ok {
		if v, ok := general["projectConfig"]; ok {
			config.General.ProjectConfig = parseBool(v, true)
		}
		if v, ok := general["startPath"]; ok {
			config.General.StartPath = stripQuotes(strings.TrimSpace(v))
		}
		if v, ok := general["useLocks"]; ok {
			config.General.UseLocks = parseBool(v, true)
		}
		if v, ok := general["useEmacsLocks"]; ok {
			config.General.UseEmacsLocks = parseBool(v, true)
		}
		if v, ok := general["layout"]; ok {
			config.General.Layout = v
		}
		if v, ok := general["mappings"]; ok {
			config.General.MappingsName = v
		}
		if v, ok := general["sequenceLength"]; ok {
			config.General.SequenceLength = parseInt(v, 4)
		}
	}

	// Storage locations. A relative scratch path resolves against the
	// layer's own directory, so a project .mew/editor.conf saying
	// scratch=backups lands the data inside that project's .mew folder.
	if storage, ok := parsed["storage"]; ok {
		if v, ok := storage["scratch"]; ok {
			v = stripQuotes(v)
			if v != "" && base != "" && !filepath.IsAbs(v) {
				v = filepath.Join(base, v)
			}
			config.Storage.Scratch = v
		}
		if v, ok := storage["backups"]; ok {
			v = stripQuotes(v)
			if v != "" && base != "" && !filepath.IsAbs(v) {
				v = filepath.Join(base, v)
			}
			config.Storage.Backups = v
		}
		if v, ok := storage["deadcat"]; ok {
			v = stripQuotes(v)
			if v != "" && base != "" && !filepath.IsAbs(v) {
				v = filepath.Join(base, v)
			}
			config.Storage.Deadcat = v
		}
		if v, ok := storage["documents"]; ok {
			v = stripQuotes(v)
			if v != "" && base != "" && !filepath.IsAbs(v) {
				v = filepath.Join(base, v)
			}
			config.Storage.Documents = v
		}
	}

	// Indicator glyphs
	if ind, ok := parsed["indicators"]; ok {
		set := func(dst *string, key string) {
			if v, ok := ind[key]; ok {
				*dst = stripQuotes(v)
			}
		}
		set(&config.Indicators.RulerFill, "rulerFill")
		set(&config.Indicators.RulerTick, "rulerTick")
		set(&config.Indicators.RulerMinor, "rulerMinor")
		set(&config.Indicators.RulerMajor, "rulerMajor")
		set(&config.Indicators.VisibleSpace, "visibleSpace")
		set(&config.Indicators.VisibleTabStart, "visibleTabStart")
		set(&config.Indicators.VisibleTabFill, "visibleTabFill")
		set(&config.Indicators.VisibleTabEnd, "visibleTabEnd")
		set(&config.Indicators.VisibleNewline, "visibleNewline")
		set(&config.Indicators.VisibleReturn, "visibleReturn")
		set(&config.Indicators.GutterEmpty, "gutterEmpty")
		set(&config.Indicators.CursorGhost, "cursorGhost")
		set(&config.Indicators.CursorOffScreen, "cursorOffScreen")
		set(&config.Indicators.TruncationLeft, "truncationLeft")
		set(&config.Indicators.TruncationRight, "truncationRight")
		set(&config.Indicators.StatPeekUp, "statPeekUp")
		set(&config.Indicators.StatPeekDown, "statPeekDown")
		set(&config.Indicators.PromptPeekUp, "promptPeekUp")
		set(&config.Indicators.PromptPeekDown, "promptPeekDown")
		set(&config.Indicators.ButtonLeft, "buttonLeft")
		set(&config.Indicators.ButtonRight, "buttonRight")
		set(&config.Indicators.ButtonShadow, "buttonShadow")
		set(&config.Indicators.FocusedButtonLeft, "focusedButtonLeft")
		set(&config.Indicators.FocusedButtonRight, "focusedButtonRight")
		set(&config.Indicators.FocusedButtonShadow, "focusedButtonShadow")
	}

	// Color sections. Key names are dynamic: [colors] is the root level,
	// [colors.<bufferType>] the buffer-type level, and [<class>.colors] the
	// window-class level. [colors.syntax] and [colors.syntax.<grammar>] are
	// syntax-highlighting class maps (jsf class -> mew color name), claimed
	// before the buffer-type rule. Section names arrive with "." mapped to "_".
	for sectionName, section := range parsed {
		switch {
		case sectionName == "colors":
			mergeStringMap(&config.Colors.Global, cleanColorSection(section))
		case sectionName == "colors_syntax" || strings.HasPrefix(sectionName, "colors_syntax_"):
			grammar := strings.TrimPrefix(strings.TrimPrefix(sectionName, "colors_syntax"), "_")
			if config.SyntaxMaps == nil {
				config.SyntaxMaps = make(map[string]map[string]string)
			}
			if config.SyntaxMaps[grammar] == nil {
				config.SyntaxMaps[grammar] = make(map[string]string, len(section))
			}
			for k, v := range section {
				// default/inherit both delete: resolution falls through to
				// the global map and built-in conventions. A blank stays: it
				// masks the mapping, forcing the grammar file's own colors.
				if keywordOf(v) != "" {
					delete(config.SyntaxMaps[grammar], strings.ToLower(k))
					continue
				}
				config.SyntaxMaps[grammar][strings.ToLower(k)] = stripQuotes(strings.TrimSpace(v))
			}
		case strings.HasPrefix(sectionName, "colors_"):
			bufType := strings.TrimPrefix(sectionName, "colors_")
			sub := config.Colors.ByType[bufType]
			mergeStringMap(&sub, cleanColorSection(section))
			config.Colors.ByType[bufType] = sub
		case strings.HasSuffix(sectionName, "_colors"):
			class := strings.TrimSuffix(sectionName, "_colors")
			sub := config.Colors.ByClass[class]
			mergeStringMap(&sub, cleanColorSection(section))
			config.Colors.ByClass[class] = sub
		}
	}

	// [match.<grammar>] / [outline.<grammar>] / [formats.<ext>] sections:
	// token pairs for go_match, definition patterns for the context
	// breadcrumb, and path-conditional format rules — each merged over the
	// built-in defaults (blank value removes one).
	for sectionName, section := range parsed {
		var table map[string]map[string]string
		var builtin map[string]map[string]string
		var grammar string
		switch {
		case strings.HasPrefix(sectionName, "match_"):
			table, builtin, grammar = config.MatchPairs, DefaultMatchPairs(), strings.TrimPrefix(sectionName, "match_")
		case strings.HasPrefix(sectionName, "outline_"):
			table, builtin, grammar = config.Outline, DefaultOutline(), strings.TrimPrefix(sectionName, "outline_")
		case strings.HasPrefix(sectionName, "formats_"):
			table, builtin, grammar = config.FormatPaths, DefaultFormatPaths(), strings.TrimPrefix(sectionName, "formats_")
		default:
			continue
		}
		m := table[grammar]
		if m == nil {
			m = make(map[string]string)
			table[grammar] = m
		}
		for k, v := range section {
			k = strings.TrimSpace(k)
			if keywordOf(v) != "" {
				// "default": restore the built-in entry (delete when the
				// built-ins have none).
				if bv, ok := builtin[grammar][k]; ok {
					m[k] = bv
				} else {
					delete(m, k)
				}
				continue
			}
			v = stripQuotes(strings.TrimSpace(v))
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
	}

	// [<class>.]options[.<grammar>][.<type>] overlays the base [options] along
	// the class/grammar/type cascade (resolved most-specific-first at apply time,
	// like the color overlays). Trichotomy: "default" -> the shipped default;
	// "inherit"/blank -> defer down the cascade; a real value wins.
	// syntax/syntaxDetect are excluded (a grammar can't pick its own detection).
	shippedOptions := defaultSectionValues("options")
	for sectionName, section := range parsed {
		class, grammar, bufType, isOpt := parseOptionsSection(sectionName)
		if !isOpt || (class == "" && grammar == "" && bufType == "") {
			continue // not an overlay (the base [options] is applied above)
		}
		if config.OptionOverlays == nil {
			config.OptionOverlays = make(map[string]map[string]string)
		}
		sig := optionOverlayKey(class, grammar, bufType)
		m := config.OptionOverlays[sig]
		if m == nil {
			m = make(map[string]string)
			config.OptionOverlays[sig] = m
		}
		for k, v := range section {
			k = strings.TrimSpace(k)
			lk := strings.ToLower(k)
			if lk == "syntax" || lk == "syntaxdetect" {
				continue // excluded from the cascade
			}
			switch keywordOf(v) {
			case "inherit":
				delete(m, lk) // defer down the cascade
				continue
			case "default":
				if dv, ok := shippedOptions[k]; ok {
					m[lk] = stripQuotes(strings.TrimSpace(dv))
				} else {
					delete(m, lk)
				}
				continue
			}
			v = stripQuotes(strings.TrimSpace(v))
			if v == "" {
				delete(m, lk) // blank defers down the cascade
				continue
			}
			m[lk] = v
		}
	}

	// [<class>.]mappings.<set>[.<type>] refine a key-mapping set by window class
	// and buffer type. Each cleaned section is stored under its signature; the
	// active set is merged across the cascade at focus time (ResolveMappingSet).
	// The base [mappings.<set>] is applied to config.Mappings above (the default
	// set); here we retain every set/refinement for per-window switching.
	for sectionName, section := range parsed {
		set, class, grammar, bufType, isMap := parseMappingsSection(sectionName)
		if !isMap {
			continue
		}
		if config.MappingSets == nil {
			config.MappingSets = make(map[string]map[string]string)
		}
		sig := mappingSetKey(set, class, grammar, bufType)
		m := config.MappingSets[sig]
		if m == nil {
			m = make(map[string]string)
			config.MappingSets[sig] = m
		}
		for k, v := range section {
			k = strings.TrimSpace(k)
			if keywordOf(v) != "" || strings.TrimSpace(stripQuotes(v)) == "" {
				delete(m, k) // unbind / defer
				continue
			}
			m[k] = v
		}
	}

	// [formats]: short-name/extension -> grammar aliases for the syntax
	// option, merged over the built-in defaults (blank value removes one;
	// "default" restores the built-in entry).
	if formats, ok := parsed["formats"]; ok {
		for k, v := range formats {
			k = strings.ToLower(strings.TrimSpace(k))
			if keywordOf(v) != "" {
				if bv, ok := DefaultFormats()[k]; ok {
					config.Formats[k] = bv
				} else {
					delete(config.Formats, k)
				}
				continue
			}
			v = stripQuotes(strings.TrimSpace(v))
			if v == "" {
				delete(config.Formats, k)
			} else {
				config.Formats[k] = v
			}
		}
	}

	// Apply key mappings. The user layer replaces the keymap wholesale
	// (removing a line from [mappings.X] really unbinds it); project layers
	// merge per key on top of the inherited map.
	mappingsKey := "mappings_" + config.General.MappingsName
	if mappings, ok := parsed[mappingsKey]; ok {
		if project {
			for k, v := range mappings {
				if keywordOf(v) != "" || strings.TrimSpace(v) == "" {
					// Unbind: the key falls back to its default handling.
					// (Bind a key to the nop command to disable it instead.)
					delete(config.Mappings, k)
					continue
				}
				config.Mappings[k] = v
			}
		} else {
			config.Mappings = mappings
			for k, v := range config.Mappings {
				if keywordOf(v) != "" || strings.TrimSpace(v) == "" {
					delete(config.Mappings, k)
				}
			}
		}
	}
}

// Config value keywords: in any section, an UNQUOTED value of "default"
// (or "system default") resets the key to mew's built-in — deleting every
// earlier layer's opinion so the system value resurfaces — and "inherit"
// explicitly defers (for the color cascade it stores the defer-blank; in
// plain tables it is the same as default). Quoting the word ("default")
// escapes the keyword and means the literal text.
const defaultSentinel = "\x00default"

// keywordOf classifies a RAW section value: "default", "inherit", or "".
func keywordOf(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	switch t {
	case "default", "system default", "system":
		return "default"
	case "inherit":
		return "inherit"
	}
	return ""
}

// defaultSectionValues returns the [<name>] section of the generated default
// configuration — the shipped value of every option, as text — memoized.
func defaultSectionValues(section string) map[string]string {
	defaultSectionsOnce.Do(func() {
		m := &Manager{}
		defaultSectionsCache = m.parseConfigFile(m.generateDefaultConfig())
	})
	return defaultSectionsCache[section]
}

var (
	defaultSectionsOnce  sync.Once
	defaultSectionsCache map[string]map[string]string
)

// normalizeScalarSection applies the value keywords to a scalar section
// (general, indicators, storage) IN PLACE: "default" substitutes the shipped
// value (or drops the key when the shipped config does not state one);
// "inherit" and blank drop the key so earlier layers show through — except
// keys in blankOK, whose empty string is itself meaningful (syntax="" turns
// highlighting off) and is kept.
func normalizeScalarSection(section, shipped map[string]string, blankOK map[string]bool) {
	for k, v := range section {
		switch keywordOf(v) {
		case "default":
			if dv, ok := shipped[k]; ok {
				section[k] = dv
			} else {
				delete(section, k)
			}
		case "inherit":
			delete(section, k)
		default:
			if strings.TrimSpace(v) == "" && !blankOK[k] {
				delete(section, k)
			}
		}
	}
}

// mergeStringMap overlays src onto *dst, allocating it when nil.
func mergeStringMap(dst *map[string]string, src map[string]string) {
	if *dst == nil {
		*dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		if v == defaultSentinel {
			delete(*dst, k)
			continue
		}
		(*dst)[k] = v
	}
}

// ProfilePath returns the mew: path of the user's profile.mew startup script.
func (m *Manager) ProfilePath() string {
	return "mew:///profile.mew"
}

// defaultProfileScript is written to profile.mew when it doesn't exist yet,
// so users discover the startup-script mechanism.
const defaultProfileScript = `#
#  |\_/|
#  >^.^<
#   ( )_~
#

macro hello (
  insert "Hello!"
);

`

// LoadProfile returns the content of the profile.mew startup script, creating
// it with a small default script when it doesn't exist yet.
func (m *Manager) LoadProfile() (string, error) {
	path := m.ProfilePath()
	if content, err := m.io().Read(path); err == nil {
		return string(content), nil
	}
	// Missing (or unreadable): create the default for discoverability.
	if err := m.io().Write(path, []byte(defaultProfileScript)); err != nil {
		return "", err
	}
	return defaultProfileScript, nil
}

// cleanColorSection lowercases keys and strips surrounding quotes from values
// in a parsed color section, keeping the key names fully dynamic. Two keyword
// values (quoted or not) are recognized: "default" behaves as if the key were
// not present at all (that level's built-in default applies), and "inherit"
// behaves as a blank value (explicitly defer to the next level down).
func cleanColorSection(section map[string]string) map[string]string {
	out := make(map[string]string, len(section))
	for k, v := range section {
		switch keywordOf(v) {
		case "default":
			// Delete at layer merge: the level's built-in default (or an
			// earlier cascade level) resurfaces. Within a single file this
			// is identical to leaving the key out.
			v = defaultSentinel
		case "inherit":
			v = "" // explicitly defer to the next cascade level
		default:
			v = stripQuotes(v)
		}
		out[strings.ToLower(k)] = v
	}
	return out
}

// stripQuotes removes a single pair of surrounding matching quotes, if present,
// and collapses a doubled quote inside ("" -> ") to a literal quote. Backslash
// escapes (\" -> ", \\ -> \) are already resolved upstream by unescapeValue, so
// all of \", "", and \\ work to embed a quote or backslash.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q == '"' || q == '\'') && s[len(s)-1] == q {
		inner := s[1 : len(s)-1]
		return strings.ReplaceAll(inner, string(q)+string(q), string(q))
	}
	return s
}

// parseConfigFile parses the config file content.
func (m *Manager) parseConfigFile(content string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	var currentSection string

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// Remove carriage returns for Windows line endings
		line = strings.TrimSuffix(line, "\r")
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines and full-line comments
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") || strings.HasPrefix(trimmedLine, ";") {
			continue
		}

		// Strip comments in the middle of the line first (findUnescapedHash is
		// quote- and section-header-aware), so a trailing comment doesn't hide
		// a section header.
		processedLine := line
		hashPos := m.findUnescapedHash(line)
		if hashPos != -1 {
			processedLine = line[:hashPos]
			// If nothing is left after removing the comment, skip this line
			if strings.TrimSpace(processedLine) == "" {
				continue
			}
		}
		trimmedLine = strings.TrimSpace(processedLine)

		// Check for section header
		if strings.HasPrefix(trimmedLine, "[") && strings.HasSuffix(trimmedLine, "]") {
			currentSection = strings.ToLower(strings.ReplaceAll(trimmedLine[1:len(trimmedLine)-1], ".", "_"))
			if _, ok := result[currentSection]; !ok {
				result[currentSection] = make(map[string]string)
			}
			continue
		}

		// Skip if no section defined yet
		if currentSection == "" {
			continue
		}

		// Parse key=value pairs
		equalPos := m.findUnescapedChar(processedLine, '=')
		if equalPos != -1 {
			key := m.unescapeValue(strings.TrimSpace(processedLine[:equalPos]))
			value := m.unescapeValue(strings.TrimSpace(processedLine[equalPos+1:]))

			result[currentSection][key] = value
		}
	}

	return result
}

// findUnescapedChar finds the position of an unescaped character outside of quoted strings.
func (m *Manager) findUnescapedChar(str string, char byte) int {
	inQuotes := false
	var quoteChar byte

	for i := 0; i < len(str); i++ {
		c := str[i]

		// Check for quote characters to track if we're inside a string
		if (c == '\'' || c == '"') && (i == 0 || str[i-1] != '\\') {
			if !inQuotes {
				inQuotes = true
				quoteChar = c
			} else if c == quoteChar {
				inQuotes = false
			}
		}

		// Only find the character if we're not inside quotes
		if !inQuotes && c == char && (i == 0 || str[i-1] != '\\') {
			return i
		} else if c == '\\' && i+1 < len(str) && !inQuotes {
			i++ // Skip escaped character outside quotes
		}
	}

	return -1
}

// findUnescapedHash finds the position of an unescaped hash outside of quoted
// strings and section headers. Only "#" starts a mid-line comment: ";" is a
// full-line comment marker only, because values in other sections (key
// mappings, scripts) need literal semicolons to separate commands.
func (m *Manager) findUnescapedHash(str string) int {
	inQuotes := false
	var quoteChar byte
	inSectionHeader := false

	// Quick check for section header format
	trimmed := strings.TrimSpace(str)
	if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
		inSectionHeader = true
	}

	for i := 0; i < len(str); i++ {
		c := str[i]

		// Check for quote characters to track if we're inside a string
		if (c == '\'' || c == '"') && (i == 0 || str[i-1] != '\\') {
			if !inQuotes {
				inQuotes = true
				quoteChar = c
			} else if c == quoteChar {
				inQuotes = false
			}
		}

		// Check for section header end
		if inSectionHeader && c == ']' {
			inSectionHeader = false
		}

		// Only find the hash if we're not inside quotes or section header
		if !inQuotes && !inSectionHeader && c == '#' && (i == 0 || str[i-1] != '\\') {
			return i
		} else if c == '\\' && i+1 < len(str) && !inQuotes {
			i++ // Skip escaped character outside quotes
		}
	}

	return -1
}

// unescapeValue unescapes special characters.
func (m *Manager) unescapeValue(value string) string {
	var result strings.Builder
	i := 0

	for i < len(value) {
		if value[i] == '\\' && i+1 < len(value) {
			next := value[i+1]
			switch next {
			case '\\':
				result.WriteByte('\\')
			case '=':
				result.WriteByte('=')
			case ',':
				result.WriteByte(',')
			case ';':
				result.WriteByte(';')
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case 'e':
				result.WriteByte(0x1b) // ESC, for ANSI color sequences
			case '0':
				result.WriteByte(0)
			case '[':
				result.WriteByte('[')
			case ']':
				result.WriteByte(']')
			case '#':
				result.WriteByte('#')
			case 'x':
				// Handle hex escape
				if i+3 < len(value) {
					hex := value[i+2 : i+4]
					if code, err := strconv.ParseInt(hex, 16, 32); err == nil {
						result.WriteByte(byte(code))
						i += 4
						continue
					}
				}
				result.WriteByte('x')
			default:
				result.WriteByte(next)
			}
			i += 2
		} else {
			result.WriteByte(value[i])
			i++
		}
	}

	return result.String()
}

// WriteDefault writes the default configuration file, and drops the shipped
// default resource files (the @included keymap sets and layouts) into
// mew:///defaults/ so they resolve and are discoverable.
func (m *Manager) WriteDefault() error {
	if err := m.io().Write(m.configPath, []byte(m.generateDefaultConfig())); err != nil {
		return err
	}
	return m.writeEmbeddedDefaults()
}

// writeEmbeddedDefaults copies each embedded defaults/*.conf into mew:///defaults/,
// skipping any that already exist so user edits are never clobbered.
func (m *Manager) writeEmbeddedDefaults() error {
	entries, err := embeddedDefaults.ReadDir("defaults")
	if err != nil {
		return nil // nothing embedded: nothing to drop
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := "mew:///defaults/" + e.Name()
		if _, err := m.io().Read(dest); err == nil {
			continue // already present — keep the user's copy
		}
		data, err := embeddedDefaults.ReadFile("defaults/" + e.Name())
		if err != nil {
			continue
		}
		if err := m.io().Write(dest, data); err != nil {
			return err
		}
	}
	return nil
}

// expandEmbeddedIncludes replaces each @include "defaults/…" line in text with
// the embedded file's contents, so the built-in config resolves entirely from
// the binary. Non-embedded includes are left for the normal disk-based
// expansion during Load.
func expandEmbeddedIncludes(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if sub := includeRe.FindStringSubmatch(strings.TrimSuffix(line, "\r")); sub != nil {
			ref := sub[1]
			if ref == "" {
				ref = sub[2]
			}
			if data, err := embeddedDefaults.ReadFile(ref); err == nil {
				out = append(out, string(data))
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// generateDefaultConfig generates the default config file content.
func (m *Manager) generateDefaultConfig() string {
	return `# mew Editor Configuration File
# This file contains settings and key mappings for the mew text editor
# Lines starting with # or ; are comments
# Hash can also be used for comments in the middle of a line

# Other configuration files can be spliced in with an @include directive (an
# at-rule, so it reads distinctly from a # comment and highlights on its own):
#   @include "relative.conf"   - relative to THIS file, via its file system
#   @include <shared.conf>     - from the standard mew config directory
# Each file is included at most once; nesting is bounded.
#
# Value keywords (in every section, at every layer): an unquoted
#   default   - reset the key to mew's built-in ("system default"), erasing
#               every earlier layer's opinion ("system default" also works)
#   inherit   - no opinion here; earlier layers (or, for colors, the next
#               cascade level) show through
# A blank value keeps its per-table meaning: it deletes [formats]/[match]/
# [outline] entries, unbinds a key mapping (bind to nop to disable a key
# instead), masks a [colors.syntax] class to the grammar's own colors, turns
# syntax="" off — and for other scalars simply inherits. Quote the word
# ("default") to mean the literal text.
#
# Projects: git-style .mew directories found walking UP from the working
# directory contribute their own editor.conf as layers over this one —
# outermost tree first, nearest last, each overriding only what it states
# (key mappings merge per key; a relative [storage] scratch resolves inside
# that project's .mew folder). A project .mew/syntax/ directory joins the
# grammar search path, nearest first. profile.mew is NEVER run from project
# directories. Set projectConfig=false below to ignore project configs.

[general]
# Structural settings: the key layout and mapping set, the project-config
# cascade, and editing locks. Runtime-settable editor options live in the
# [options] section below.
#
# Layer project .mew/editor.conf files (found walking up from the working
# directory) over this configuration
projectConfig=true
# Where a session starts when its working directory cannot be derived any
# other way (GUI launches begin at the filesystem root). The editor prefers
# the current document's own location, then this path, then your home.
# startPath=~/projects

# Editing locks: useLocks=false disables all locking. useEmacsLocks controls
# the emacs-interoperable ".#<name>" lock files specifically (skipped
# automatically inside git repos whose .gitignore does not cover them); when
# emacs locks are off or skipped, mew keeps its own lock in the nearest .mew
# directory instead.
useLocks=true
useEmacsLocks=true
layout=qwerty
mappings=mew

[options]
# Runtime-settable editor options. Each option here is exactly a set_option
# command and a command-line --flag. Per-window options (tabSize,
# showLineNumbers, showInvisibles, showBidi, showColumnRuler, direction) can
# differ from buffer to buffer; the rest are editor-wide.
#
# [options.<grammar>] overlays these per-window options for buffers of a given
# syntax grammar, the same way [colors.syntax.<grammar>] overlays colors — e.g.
# [options.markdown] wordless prose settings, [options.go] tabSize=4. A value
# overrides; "inherit" (or blank) defers to the base [options]; "default"
# resolves to the shipped default. syntax and syntaxDetect are excluded (a
# grammar cannot pick the detection that selected it). A value the user sets at
# runtime (set_option) or per file on the command line is pinned and not
# overwritten by the overlay.
#
# [options.go]
# tabSize=4
# showInvisibles=true
showLineNumbers=true
showColumnRuler=true
# Highlight the ruler cells under the cursor (and its ghost / bidi companion
# cursor) with the rulerCursor color
rulerShowsCursor=false
tabSize=4
showInvisibles=false
# Show direction markers at the leading edge of each directional fragment
# ("<" entering RTL, ">" entering LTR); explicit direction controls render
# as their own marker
showBidi=false
# Show a "*" (in the "marks" color) at every mark/decoration position in the
# text: no (off), yes (user marks), or all (also mew's internal marks). Boolean
# aliases (on/off/true/false) are accepted as no/yes.
showMarks=no
# Insert mode: yes types text in (the default). no turns on overwrite mode,
# where typing replaces the character under the caret — except at the end of a
# line, where it still appends. Per window.
insertMode=yes
# Read-only: yes rejects edits made through the window (typing, deleting,
# replacing, pasting); movement, search, marks, and undo/redo still work. Per
# window.
readOnly=no
# Hyperlink layer for grammar-recognized links (dokuwiki): links paint in the
# link color, and when the caret enters one the window switches to browse
# mode, rendering links as buttons (nav_cancel — ^C's first stop — exits).
# no disables all of it: links render exactly as the grammar colors them.
# Per window.
linkBrowsing=yes
# Syntax highlighting: the name of a jsf grammar file ("cpp", "go", ...),
# searched in ~/.mew/syntax/, mew's built-in set, then any installed JOE
# syntax directories. Empty (or "none") disables highlighting.
syntax=""
# Auto-detect each buffer's grammar: a #! shebang on the first line wins,
# else the buffer's file extension (or extensionless basename like
# Makefile), both resolved through the [formats] table. Buffers that
# detect nothing fall back to the syntax option above.
syntaxDetect=false
# Grammar flavors (space-separated) whose highlighter should ignore the
# document's own project .mew/syntax folder and resolve from your copy in
# ~/.mew/syntax, the built-in set, or JOE instead. Use this when a project
# ships a grammar you would rather override with your own — e.g.
# syntaxOverrides="go conf". Descends through the option overlay, so it can be
# set for one window or buffer too.
syntaxOverrides=""
# go_match fallback context for buffers with NO syntax grammar: enabled
# constructs read as strings/comments and are skipped when matching (a
# grammar's real highlighter context supersedes these). Quotes do not span
# lines; slashStar does.
matchIgnoresSingleQuote=false
matchIgnoresDoubleQuote=true
matchIgnoresSlashStar=false
matchIgnoresSlashSlash=false
matchIgnoresHash=false
matchIgnoresDoubleHyphen=false
matchIgnoresSemicolon=false
matchIgnoresPercent=false
# macOS Option-key layer: auto (decode Option chars to M- keys on macOS),
# true (everywhere), false (off). Unless false, unmapped M- keys re-insert
# the Option character, so bindings steal combos and typing stays seamless
# (Alt on any platform gains the mac character layer).
macOptionKeys=auto
wordWrap=false
searchIgnoreCase=false
searchWrap=true
searchRegex=false
modebarLocation=top
# Modebar templates: text with %CODE% substitutions (%% = a literal %). Inner
# is the filename region, Default the middle text shown when no live context
# (key-sequence completion or outline breadcrumb) applies, Outer the right-hand
# readout (ellipsized when it does not fit). Codes: %FN% %FORTUNE% %FRAG%
# %HEAP% %LINE% %RUNE% %COL% %LINEBYTE% %ABSBYTE%.
modebarInner=%FN%
modebarDefault=%FORTUNE%
modebarOuter=Frag:%FRAG% Heap:%HEAP% Line:%LINE% Rune:%RUNE%
# Async timeouts in seconds (0 = never): promptTimeout bounds how long an
# unanswered prompt keeps its command sequence resumable, scriptTimeout
# bounds other script async operations
promptTimeout=300
scriptTimeout=300
# How far page up/down move: a fixed count (24), a percentage (25%), a
# Paging: optimal distance (a count like 24, or a percentage like 50%), how
# much context to keep between pages (a count or a percentage, 0 = none), and a step to
# round the distance down to a multiple of (e.g. 6 snaps to 12 or 24).
pageSizeOptimal=100%
pageOverlapMinimum=1
pageSizeStep=0
# Largest count repeat_next will honor (a bigger request is clamped to this)
maxRepeat=100
# How many kill-ring entries are kept (oldest evicted past this)
killRingEntries=10
# Base text direction each line begins in: ltr or rtl (RTL segments within a
# line render bidirectionally either way)
direction=ltr
# Terminals that apply their own bidi reordering (macOS Terminal.app) re-flip
# mew's already-visual RTL output; "true" emits RTL runs in logical order so
# such terminals lay them out correctly, "false" keeps visual order (correct
# for stream-order terminals: iTerm2, xterm, most). "auto" probes the terminal
# once, on the first frame containing RTL content, and decides from its answer
# (falling back to the TERM_PROGRAM environment).
flipBidiForHost=auto

[layout.qwerty]
#
# use | to mark absolute middle, and/or < and > to mark left/right hands
# use || and/or << and >> to mark areas beyond convenient reach
#
row+3="esc  F1  F2  F3  F4  F5  F6||F7  F8  F9  F10  F11  F12     fdel"
row+2="` + "`" + `   1   2   3   4   5<  6  >7   8   9   0>> -   =    del   home"
row+1="tab   q   w   e   r < t | y > u   i   o   p>> [   ]   \\    pgup"
row-H="^   a   s   d   f   g | h   j   k   l   ;   '  \\"  return>>pgdn"
row-1="S-    z   x   c   v<  b > n   m   ,   .   /   S- >>  up     end"
row-2="^   M-      <       space       >      M- >>  left  down  right"

[storage]
# Local storage locations. scratch is the directory handed to Garland for
# cold storage of large files; empty (or omitted) means the system temp dir.
# backups is where automatic pre-session backups land; empty means
# ~/.mew/backups. deadcat is where the crash/kill dump of modified buffers
# lands (mew's DEADJOE); empty means the folder editor.conf loaded from
# (normally ~/.mew), with a breadcrumb copy left in the working directory.
# documents is the fallback directory filename completion globs for a buffer
# with no file of its own; empty falls back to the launch directory.
# A relative path in a project layer resolves inside that project's .mew.
# Only honored from this local config file, never from host-supplied config.
scratch=
backups=
deadcat=
documents=

[indicators]
# Glyphs/labels used to draw editor chrome. Values are quoted.
# The peek labels (statPeek*/promptPeek*) run through the same %CODE%
# substitution engine as the modebar templates: %SPU% %SPD% %PPU% %PPD%
# resolve to the key currently bound to stat_peek_up / stat_peek_down /
# prompt_peek_up / prompt_peek_down, in the spelling the binding is stored
# under, so the hint always matches the live keymap.
rulerFill="░"
rulerTick="."
rulerMinor=":"
rulerMajor="|"
visibleSpace="·"
visibleTabStart=":"
visibleTabFill="-"
visibleTabEnd=">|"
visibleNewline="⮐"
visibleReturn="M"
gutterEmpty="~"
cursorGhost="|"
cursorOffScreen="@"
truncationLeft="<"
truncationRight=">"
statPeekUp="[%SPU%]"
statPeekDown="[%SPD%]"
promptPeekUp="[%PPU%]"
promptPeekDown="[%PPD%]"
# Link-as-button chrome (browse mode): a link renders as
# buttonLeft + title + buttonRight + buttonShadow; the focused* variants
# apply to the button the caret is inside.
buttonLeft=" "
buttonRight=" "
buttonShadow="▐"
focusedButtonLeft="<"
focusedButtonRight=">"
focusedButtonShadow="█"

[colors]                          # root-level defaults
reset="\e[0m"                     # reset to default
messages="\e[0;1;37;44m"          # silver on blue
text="\e[0;37;40m"                # silver on black
invisibles="\e[0;1;40;90m"        # bright black / dark gray on black
cursorGhost="\e[0;30;100m"        # black on dark gray
cursorOffScreen="\e[0;30;42m"     # black on green
truncation="\e[0;37;41m"          # silver on red
hint="\e[0;97;44m"                # bright white on blue - peek indicator hints
special="\e[33m"                  # yellow foreground - control code substitutes
marks="\e[0;91m"                  # bright red
notes="\e[0;36;40m"               # cyan on black
lineNumbers="\e[1;96;44m"         # aqua on blue
selection="\e[0;30;47m"           # black text on silver
selectionInvisibles="\e[1;30;47m" # dark gray on silver
rulerEnds="\e[0;97;45m"         # bright white on magenta (used for end numbers)
rulerFill="\e[0;37;45m"           # silver on magenta (for the fill glyph)
rulerTick="\e[0;37;45m"           # silver on magenta (for ".")
rulerMinor="\e[0;93;45m"          # bright yellow on magenta (for ":")
rulerMajor="\e[0;92;45m"          # bright green on magenta (for "|" or regular numbers)
rulerCursor="\e[0;30;47m"         # black on silver (cursor columns when rulerShowsCursor)
# Hyperlinks (grammar-derived links, e.g. dokuwiki). Caret mode paints link
# source text in "link"; browse mode renders links as buttons (button/
# buttonShadow, with the *Focused variants on the button the caret occupies).
# linkRecent is reserved for recently-followed links.
link="\e[0;4;93;40m"              # underlined bright yellow on black
linkRecent="\e[0;4;32;40m"        # underlined green on black
linkHover="\e[0;4;92;40m"        # underlined bright green on black (pointer over)
# Dokuwiki heading base color (browse mode adds bold/underline per level).
heading="\e[0;96;40m"             # bright cyan on black
button="\e[0;30;47m"              # black on silver
buttonRecent="\e[0;30;42m"        # black on dark green (a visited link)
buttonShadow="\e[0;90;47m"        # dark gray on silver
buttonShadowRecent="\e[0;90;42m"  # dark gray on dark green
buttonFocused="\e[0;30;46m"       # black on cyan
buttonShadowFocused="\e[0;90;46m" # dark gray on cyan
buttonPressed="\e[0;97;44m"       # bright white on blue (mouse held down)
buttonShadowPressed="\e[0;37;44m" # silver on blue
buttonHover="\e[0;93;45m"         # bright yellow on purple (pointer over)
buttonShadowHover="\e[0;90;45m"   # dark gray on purple
syntaxComment="\e[0;32;40m"       # green on black
syntaxString="\e[0;36;40m"        # cyan on black
syntaxEscape="\e[0;96;40m"      # bright cyan on black
syntaxConstant="\e[0;91;40m"      # bright red on black (numbers, literals)
syntaxKeyword="\e[0;1;97;40m"     # bold bright white on black
syntaxType="\e[0;93;40m"          # bright yellow on black
syntaxPreproc="\e[0;94;40m"       # bright blue on black
syntaxBad="\e[0;97;41m"         # bright white on red

# Syntax highlighting maps a grammar's color classes onto the systematic
# syntax* colors above. [colors.syntax] adjusts the mapping for every
# grammar; [colors.syntax.<name>] overrides it for one grammar, e.g.:
#
# [colors.syntax.cpp]
# Preproc = syntaxKeyword

# [formats] maps short format names and file extensions onto grammar files
# for the syntax option (built-in aliases like c=cpp, js=javascript,
# py=python, sh=shell, md=markdown are merged first; a blank value removes
# one), e.g.:
#
# [formats]
# vue = javascript
# inc =

# [formats.<ext>] refines an extension by LOCATION: path patterns mapped to
# grammars, consulted before the plain extension rule when syntaxDetect
# resolves a file. A pattern without a slash matches any single path
# component ("pages" = inside a folder named pages; "*wiki*" = wiki
# anywhere in a component); with a slash it matches the whole path, '*'
# crossing separators. Matching is case-insensitive. Built-in default:
# .txt under *wiki* or pages highlights as dokuwiki. Example:
#
# [formats.txt]
# *notes*/journal = markdown
# pages =

# [match.<grammar>] adds token pairs to go_match beyond ()[]{} — opener =
# closer entries; openers sharing a closer nest as one family, and the
# special entry tags = true enables <tag></tag> matching. Built-in defaults
# cover shell (if=fi, case=esac, do=done), lua, make, cpp (\#if=\#endif),
# tex (\\begin=\\end), and html/php tags. Example:
#
# [match.shell]
# select = done

# [outline.<grammar>] defines the definition patterns behind the modebar's
# context breadcrumb (the enclosing function/class/section chain at the
# caret). Each value is a regular expression (backslashes doubled) whose
# LAST capture group is the name; an optional first group of whitespace or
# '#' characters sets the nesting depth, otherwise the line's indentation
# is used. Built-in defaults cover the bundled grammars. Example:
#
# [outline.python]
# route = ^([ \t]*)@app\\.route.*def\\s+(\\w+)

[colors.work]             # defaults for workbuffers
text="\e[0;1;46;97m"      # bright white on cyan
messages="\e[0;1;43;97m"  # bright white on amber

[colors.prompt]           # defaults for promptbuffers
messages="\e[0;1;42;93m"  # bright yellow on green
text="\e[0;1;42;97m"      # bright white on green

[modebar.colors]       # the modebar class - a specific workbuffer type
text="\e[0;44m"        # silver on blue - modebar fill
messages="\e[1;96;44m" # aqua on blue - stats readout (Frag/Heap/Line/Rune)
modifiers="\e[0;44m"   # silver on blue - active modifiers & space before & after
buffer="\e[0;93;44m"   # bright yellow on blue - buffer name (filename)
completion="\e[0;44m"  # silver on blue - autocompletion (and space before and after)
context="\e[0;92;44m"  # bright green on blue - context (when autocompletion isn't showing)
logo="\e[1;97;41m"     # bright white on red - M_ logo

[notification.colors]
messages="\e[0;37;43m"

[warning.colors]
messages="\e[0;93;43m"                # bright yellow on brown

[error.colors]
messages="\e[0;97;41m"                # bright white on red

[mappings.mew]

# -- first, include all the mew defaults --
@include "defaults/keys_cursor_movement.conf"
@include "defaults/keys_editing.conf"
@include "defaults/keys_quick_menu.conf"
@include "defaults/keys_block_menu.conf"
@include "defaults/keys_buffer_and_save_menus.conf"
@include "defaults/keys_options_menu.conf"
@include "defaults/keys_backcompat.conf"

# -- override as needed --
^K H    =help_toggle
esc X   =cmd
esc y   =kill_ring_yank
esc Y   =kill_ring_pop
^@ U    =stat_peek_up
^@ V    =stat_peek_down
^@ P    =prompt_peek_up
^@ N    =prompt_peek_down
^@ O    =editor_options
^@ ,    =window_prior
^@ .    =window_next

tab     =nav_next|completion|insert '\t'
S-tab   =nav_prior
return  =nav_follow|accept|insert '\n'
^C      =nav_cancel|cancel|buffer_close
^R      =repeat_next

esc >   =scroll_right
esc <   =scroll_left

^L      =find_next
^]      =go_match

^_      =buffer_undo
# ^/ is straight undo you can lean on (Emacs/JOE muscle memory, and its own
# key under the kitty keyboard protocol where it no longer collapses onto ^_).
# It sits at the right end of the bottom row, mirroring ^Z at the left end,
# whose redo-first fallback ping-pongs undo/redo forever the way a Windows
# user expects.
^/      =buffer_undo
^Z      =buffer_redo|buffer_undo
`
}

// parseBool parses a string as boolean with a default.
func parseBool(s string, defaultVal bool) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "true" || s == "1" || s == "yes" || s == "on" {
		return true
	}
	if s == "false" || s == "0" || s == "no" || s == "off" {
		return false
	}
	return defaultVal
}

// ParseShowMarks normalizes a showMarks value to its canonical enum form: "no"
// (off), "yes" (user-visible marks), or "all" (also mew's internal, underscore-
// prefixed marks). Boolean aliases are accepted so older configs keep working
// (on/true/1 -> yes, off/false/0 -> no). ok is false for anything unrecognized.
func ParseShowMarks(s string) (value string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "all":
		return "all", true
	case "yes", "on", "true", "1":
		return "yes", true
	case "no", "off", "false", "0":
		return "no", true
	}
	return "", false
}

// parseInt parses a string as integer with a default.
func parseInt(s string, defaultVal int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return defaultVal
}
