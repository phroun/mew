// Package hostcfg loads launch configuration for the KittyTK display
// hosts (kittytk-sdl, kittytk-tui) from a plain kittytk.ini, so a
// non-technical user can configure the app by editing a text file
// instead of passing command-line arguments.
//
// The first kittytk.ini found in this order wins (whole file; later
// locations are a fallback, not merged):
//
//  1. the current working directory
//  2. the directory holding the executable
//  3. the user config dir (%APPDATA%\kittytk on Windows, else
//     $XDG_CONFIG_HOME/kittytk or ~/.config/kittytk)
//
// The file is section-tolerant: keys are matched by name, so section headers
// are cosmetic and a user who omits them still gets a working config. The one
// exception is native, which is read per section - under [system] it styles
// the graphical host's menu shortcuts, under [tui] the terminal host's - so
// the two hosts can be configured independently:
//
//	[window]
//	title        = KittyTK
//	width        = 1024
//	height       = 768
//	scale        = 2
//	font_size    = 12
//	border_width =            ; window frame thickness in device px (blank/0 = default)
//	fps          =            ; true = show the render frame rate in the graphical
//	                          ;        host's OS title bar (kittytk-sdl only)
//	renderer     =            ; rendering backend: software (default) or webgpu
//	                          ;   (webgpu requires building with -tags webgpu)
//	host_type    =            ; force the desktop the keymap's (kde) / (gnome) /
//	                          ;   … hints are tested against, overriding what the
//	                          ;   session advertises (blank = detect)
//	fonts_path   =            ; extra font search directories (comma list, relative
//	                          ;   to this ini) the engine scans to find families by name
//	ui_term      =            ; the ui-term terminal face (family or comma fallback
//	                          ;   list). Any ui_* key re-points the matching font
//	                          ;   alias: ui_text_serif, ui_term_hebrew, ui_term_cjk_sans,
//	                          ;   … — the whole ui-{text,term}-{script}-{style} tree.
//
//	[fonts]                   ; family name -> font file path (graphical host only;
//	JetBrainsMono = /path/to/JetBrainsMono.ttf   ; relative paths resolve against this ini)
//
//	[service]
//	endpoint =            ; blank = default; tcp://host:port, tls://…, or a socket path
//	token    =            ; optional shared secret
//
//	[system]
//	native   =            ; graphical host menu-shortcut glyph style:
//	                      ;   true = native glyphs (⌃⌥⇧⌘) only when the host OS is macOS
//	                      ;   mac  = force native glyphs on any OS
//	                      ;   else = the default compact notation (^X, M-x, S-Tab)
//
//	[tui]
//	native    =           ; same knob for the terminal host (independent of [system])
//	clipboard =           ; terminal clipboard integration:
//	                      ;   blank/osc52/system = mirror Copy/Cut to the terminal
//	                      ;                         clipboard via OSC 52 (the default)
//	                      ;   osc52-paste/full    = also query the terminal on Paste
//	                      ;                         (read-back), falling back to the
//	                      ;                         internal clipboard if it is silent
//	                      ;   internal/off        = host-internal clipboard only
//	pseudofont_black_serif = ; text-backend cipher pseudo-fonts, selected by
//	pseudofont_black_sans  = ;   NAME (Unicode math-alphanumerics faking a
//	pseudofont_double      = ;   style). Each defaults on; set = off renders
//	pseudofont_fraktur     = ;   that style as plain text instead.
//	pseudofont_script      = ;
//	fraktur_mode           = ; SEPARATE: how a terminal's VT100 fraktur request
//	                      ;   (font 20 / SGR 20) is handled — native (forward the
//	                      ;   escape to the enclosing terminal), pseudo (the
//	                      ;   fraktur cipher; default), off (normal font).
//
// Environment variables still take precedence over the file: KITTYTK_DISPLAY
// for the endpoint and KITTYTK_TOKEN for the token.
package hostcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
)

// IniName is the configuration file basename both hosts look for.
const IniName = "kittytk.ini"

// Config is the resolved launch configuration. Window fields apply only
// to graphical hosts (kittytk-sdl); the terminal host ignores them.
type Config struct {
	Title       string // window title bar text
	Width       int    // window width in pixels
	Height      int    // window height in pixels
	Scale       int    // pixels per abstract unit (1 = small, 2 = crisp/large)
	FontSize    int    // UI font point size; sizes the desktop cell grid (12 = default)
	BorderWidth int    // graphical window-frame border width in device pixels, reserved outside the content (0 = default)
	ShowFPS     bool   // show the render frame rate in the graphical host's OS title bar
	VSync       bool   // graphical host: sync presents to the display refresh (default true; false uncaps fps)
	Renderer    string // rendering backend: "software" (default) or "webgpu" (requires -tags webgpu build)

	// Wallpaper is a path to a PNG/JPEG/GIF tiled across the desktop
	// ([window] wallpaper=, --wallpaper=PATH, or KITTYTK_WALLPAPER).
	// Empty keeps the built-in 8x8 pattern. The image is REPEATED from
	// the surface origin, not stretched, so its size is free to be
	// anything.
	Wallpaper string

	// WallpaperMode / Tiling / Align / Filter are the layout names as
	// configured; Scale multiplies the size the mode arrives at. Empty
	// strings and a zero scale mean "leave the default alone", so a
	// partially-configured [window] section does not have to restate
	// everything.
	WallpaperMode   string
	WallpaperTiling string
	WallpaperAlign  string
	WallpaperFilter string
	WallpaperScale  float64

	Endpoint string // service endpoint ("" = the conventional default)
	Token    string // optional shared secret

	// Native/TUINative set the menu-shortcut glyph style ("true" = native on
	// macOS, "mac" = force native, else default) for the graphical ([system])
	// and terminal ([tui]) hosts respectively.
	Native    string
	TUINative string

	// TUIPseudoFontsDisabled turns off individual cipher pseudo-fonts (used BY
	// NAME) in the text backend ([tui] pseudofont_<group> = off): keyed by
	// toggle group (black_serif, black_sans, double, fraktur, script). A
	// disabled group renders plain instead of the styled Unicode. Absent =
	// enabled.
	TUIPseudoFontsDisabled map[string]bool

	// TUIFrakturMode ([tui] fraktur_mode) is a SEPARATE concern: how a
	// terminal's VT100 fraktur REQUEST (font 20 / SGR 20) is handled — "native"
	// forwards the VT fraktur escape to the enclosing terminal, "pseudo" renders
	// it with the fraktur cipher (default), "off" ignores it (normal font).
	// Empty = the backend default (pseudo). Distinct from pseudofont_fraktur.
	TUIFrakturMode string

	// TUIClipboard controls the terminal host's clipboard integration, set by
	// the [tui] section's `clipboard` key. "internal"/"off"/"none"/"false"
	// keep an internal-only clipboard; anything else (or empty) mirrors
	// Copy/Cut to the terminal's clipboard via OSC 52.
	TUIClipboard string

	// Fonts maps a font family name to a font file path, read from the [fonts]
	// section (keys keep their original case — a family name). Registered into
	// the graphical host's shared text engine at startup so any name (including
	// the ui-term terminal face) resolves against it. Relative paths resolve
	// against the ini's directory. Ignored by the terminal host (fonts aren't
	// real there).
	Fonts map[string]string

	// FontsPath lists extra directories the font engine scans to resolve
	// families by NAME, from [window] fonts_path (comma-separated). Relative
	// entries resolve against the ini's directory.
	FontsPath []string

	// Mappings is the default key registry: a key or key sequence as the input
	// layer reports it, mapped to the command it runs. Read from the
	// [mappings] section, where — as in [fonts] — the names on the LEFT are
	// DATA, so the section is read by section rather than by key name.
	//
	// Keys keep their case: "S-Tab" and "s-Tab" are Shift-Tab and Super-Tab,
	// two different chords. Order within the file is not significant; a later
	// line for the same key replaces an earlier one.
	Mappings map[string]string

	// HostType overrides the desktop environment the keymap's environment hints
	// are tested against, read from [window] host_type. The session normally
	// says what it is (XDG_CURRENT_DESKTOP), so this is for where it says
	// nothing, says the wrong thing, or where someone wants a Mac's keymap on
	// a Linux desktop for the afternoon. Blank keeps whatever was detected.
	HostType string

	// AcceleratorChord is the pattern a menu accelerator is formed from, read
	// from [window] accelerator_chord. The token "*" is replaced by a menu's
	// mnemonic letter wherever it sits, so "M-*" forms "M-h" for &Help and
	// "^X * Return" forms "^X H Enter". Blank disables chord accelerators
	// without touching the bare-letter mnemonics, which are ordinary typing
	// handled by a focused menu bar and never enter a registry.
	AcceleratorChord string

	// FontAliases holds the [window] ui_* font-alias overrides: any key of the
	// form ui_<...> re-points the font alias ui-<...> (underscores -> hyphens)
	// at a comma-separated fallback list — the whole systematic font tree,
	// overridable at any level (ui_term, ui_text_serif, ui_term_hebrew_sans, …).
	// Keyed by the hyphenated alias name.
	FontAliases map[string][]string

	// Source is the path of the ini that was loaded, or "" if none was
	// found (defaults were used).
	Source string
}

// Defaults returns the built-in configuration used when no ini is found
// (and as the base every ini is applied onto).
func Defaults() Config {
	return Config{Title: "KittyTK", Width: 1024, Height: 768, Scale: 2, FontSize: 12, VSync: true, Renderer: "software"}
}

// SearchPaths returns the ordered candidate ini paths (see the package
// doc). Unreadable directories are simply skipped.
func SearchPaths() []string {
	var ps []string
	if wd, err := os.Getwd(); err == nil {
		ps = append(ps, filepath.Join(wd, IniName))
	}
	if exe, err := os.Executable(); err == nil {
		ps = append(ps, filepath.Join(filepath.Dir(exe), IniName))
	}
	ps = append(ps, filepath.Join(client.ConfigDir(), IniName))
	return ps
}

// Load returns the configuration from the first readable kittytk.ini in
// SearchPaths (whole file wins), or Defaults() if none is found.
func Load() Config {
	cfg := Defaults()
	for _, p := range SearchPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		apply(data, &cfg)
		cfg.Source = p
		// Resolve relative font paths against the ini's own directory, so a
		// user can ship fonts next to their kittytk.ini.
		if dir := filepath.Dir(p); dir != "" {
			for family, fp := range cfg.Fonts {
				if !filepath.IsAbs(fp) {
					cfg.Fonts[family] = filepath.Join(dir, fp)
				}
			}
			for i, sp := range cfg.FontsPath {
				if !filepath.IsAbs(sp) {
					cfg.FontsPath[i] = filepath.Join(dir, sp)
				}
			}
		}
		break // first found wins
	}
	return cfg
}

// splitList parses a comma-separated value (font families or paths), stripping
// surrounding quotes off the whole value and off each element, dropping empties.
// normalizeUITargets rewrites underscores to hyphens in any entry naming
// another UI alias, so the underscored spelling works on BOTH sides of a
// ui_* line: ui_term_hebrew = ui_term_hebrew_sans reads naturally even though
// the internal alias is ui-term-hebrew-sans. Only entries naming the ui tree
// are touched — a real font family may contain underscores.
func normalizeUITargets(names []string) []string {
	for i, n := range names {
		if l := strings.ToLower(n); strings.HasPrefix(l, "ui_") || strings.HasPrefix(l, "ui-") {
			names[i] = strings.ReplaceAll(n, "_", "-")
		}
	}
	return names
}

func splitList(v string) []string {
	v = stripQuotes(strings.TrimSpace(v))
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = stripQuotes(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stripQuotes removes a matching pair of surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// apply parses ini text and sets the recognized keys on cfg. Section
// headers are tolerated but ignored; keys are matched by name (case-
// insensitive). Unknown keys and malformed numbers are skipped so a
// stray typo never prevents the host from starting.
func apply(data []byte, cfg *Config) {
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		// Track the current section: keys are matched by name (sections are
		// cosmetic), except native, which is routed by section below.
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = strings.ToLower(strings.TrimSpace(line[1:end]))
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		origKey := strings.TrimSpace(line[:eq])
		key := strings.ToLower(origKey)
		val := strings.TrimSpace(stripInlineComment(line[eq+1:]))
		// [fonts] is the one section whose keys are DATA (family names, kept in
		// their original case), not fixed knobs — a family -> file path map.
		if section == "fonts" {
			if origKey == "" {
				continue
			}
			v := stripQuotes(val)
			if v == "" {
				continue
			}
			if cfg.Fonts == nil {
				cfg.Fonts = map[string]string{}
			}
			cfg.Fonts[origKey] = v
			continue
		}
		// [mappings] is the other section whose keys are DATA — a key name, not
		// a setting name — so it is read by section and the case is kept:
		// "S-Tab" and "s-Tab" are two different chords. An empty value unbinds
		// the key rather than being ignored, which is how a user turns a
		// default off without having to know what it was.
		if section == "mappings" {
			if origKey == "" {
				continue
			}
			if cfg.Mappings == nil {
				cfg.Mappings = map[string]string{}
			}
			cfg.Mappings[origKey] = stripQuotes(val)
			continue
		}
		// Any ui_* key re-points the font alias ui-* (underscores -> hyphens) at
		// a comma-separated fallback list — the whole systematic font tree.
		if strings.HasPrefix(key, "ui_") {
			alias := strings.ReplaceAll(key, "_", "-")
			if list := normalizeUITargets(splitList(val)); len(list) > 0 {
				if cfg.FontAliases == nil {
					cfg.FontAliases = map[string][]string{}
				}
				cfg.FontAliases[alias] = list
			} else {
				delete(cfg.FontAliases, alias)
			}
			continue
		}
		// [tui] font knobs (two separate things): pseudofont_<group> = off
		// disables a by-name cipher pseudo-font; fraktur_mode = native|pseudo|off
		// governs how a terminal's VT100 fraktur request is rendered.
		if section == "tui" {
			if strings.HasPrefix(key, "pseudofont_") {
				group := strings.TrimPrefix(key, "pseudofont_")
				if cfg.TUIPseudoFontsDisabled == nil {
					cfg.TUIPseudoFontsDisabled = map[string]bool{}
				}
				cfg.TUIPseudoFontsDisabled[group] = isFalsey(val) // off/false/no/0
				continue
			}
			if key == "fraktur_mode" {
				switch strings.ToLower(val) {
				case "native", "pseudo", "off":
					cfg.TUIFrakturMode = strings.ToLower(val)
				}
				continue
			}
		}
		switch key {
		case "title":
			cfg.Title = val
		case "accelerator_chord":
			cfg.AcceleratorChord = stripQuotes(val)
		case "host_type":
			cfg.HostType = stripQuotes(val)
		case "width":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Width = n
			}
		case "height":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Height = n
			}
		case "scale":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Scale = n
			}
		case "font_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.FontSize = n
			}
		case "border_width":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.BorderWidth = n
			}
		case "fps":
			cfg.ShowFPS = parseBool(val)
		case "vsync":
			// Default-on flag: only an explicit falsey value disables it, so a
			// blank "vsync =" keeps the default (sync to refresh).
			cfg.VSync = !isFalsey(val)
		case "renderer":
			// Rendering backend: "software" (default) or "webgpu"
			switch strings.ToLower(val) {
			case "software", "webgpu":
				cfg.Renderer = strings.ToLower(val)
			}
		case "wallpaper":
			cfg.Wallpaper = val
		case "wallpaper_mode":
			cfg.WallpaperMode = val
		case "wallpaper_tile":
			cfg.WallpaperTiling = val
		case "wallpaper_align":
			cfg.WallpaperAlign = val
		case "wallpaper_filter":
			// Named "filter" rather than "scaling" so it cannot be
			// misread as a typo for wallpaper_scale, which is a NUMBER
			// and means something entirely different.
			cfg.WallpaperFilter = val
		case "wallpaper_scale":
			if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
				cfg.WallpaperScale = f
			}
		case "endpoint":
			cfg.Endpoint = val
		case "token":
			cfg.Token = val
		case "native":
			// The only section-sensitive key: [tui] configures the terminal
			// host, every other section (including none) the graphical host.
			if section == "tui" {
				cfg.TUINative = val
			} else {
				cfg.Native = val
			}
		case "clipboard":
			// Terminal-host clipboard integration, read under [tui].
			if section == "tui" {
				cfg.TUIClipboard = val
			}
		case "fonts_path":
			// [window] fonts_path: extra font search directories (comma list).
			cfg.FontsPath = append(cfg.FontsPath, splitList(val)...)
		}
	}
}

// parseBool reads a permissive boolean: true/1/yes/on (case-insensitive) are
// true, everything else (including blank) is false.
func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// isFalsey reports whether a value explicitly disables a default-on flag:
// false/0/no/off (case-insensitive). Blank is NOT falsey, so an empty value
// keeps the default.
func isFalsey(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no", "off":
		return true
	default:
		return false
	}
}

// stripInlineComment removes a trailing `;`/`#` comment from a value. A
// comment starts only where the marker begins the value or follows
// whitespace, so a value that itself contains ';' or '#' with no leading
// space (a token like "a;b", a "#rrggbb" is not a hostcfg value) is kept.
func stripInlineComment(v string) string {
	for i := 0; i < len(v); i++ {
		if c := v[i]; c == ';' || c == '#' {
			if i == 0 || v[i-1] == ' ' || v[i-1] == '\t' {
				return v[:i]
			}
		}
	}
	return v
}

// WallpaperEnv names the environment variable that overrides the
// configured wallpaper. It is the quickest way to try one: no ini edit,
// no rebuild, just a path on the command line.
const WallpaperEnv = "KITTYTK_WALLPAPER"

// ResolveWallpaperLayout folds the configured names into a layout,
// starting from the default so an unset key keeps it. Names that do not
// parse are reported rather than silently ignored: a misspelt mode
// papering the desktop the old way with no explanation is the worst of
// both.
func (c Config) ResolveWallpaperLayout() (core.WallpaperLayout, []error) {
	l := core.DefaultWallpaperLayout
	var errs []error

	if c.WallpaperMode != "" {
		if m, ok := core.ParseWallpaperMode(strings.ToLower(c.WallpaperMode)); ok {
			l.Mode = m
		} else {
			errs = append(errs, fmt.Errorf("unknown wallpaper_mode %q", c.WallpaperMode))
		}
	}
	if c.WallpaperTiling != "" {
		if tl, ok := core.ParseWallpaperTiling(strings.ToLower(c.WallpaperTiling)); ok {
			l.Tiling = tl
		} else {
			errs = append(errs, fmt.Errorf("unknown wallpaper_tile %q", c.WallpaperTiling))
		}
	}
	if c.WallpaperAlign != "" {
		if a, ok := core.ParseWallpaperAlignment(c.WallpaperAlign); ok {
			l.Align = a
		} else {
			errs = append(errs, fmt.Errorf("unknown wallpaper_align %q", c.WallpaperAlign))
		}
	}
	if c.WallpaperFilter != "" {
		if smooth, ok := core.ParseWallpaperFilter(strings.ToLower(c.WallpaperFilter)); ok {
			l.Smooth = smooth
		} else {
			errs = append(errs, fmt.Errorf("unknown wallpaper_filter %q", c.WallpaperFilter))
		}
	}
	if c.WallpaperScale > 0 {
		l.Scale = c.WallpaperScale
	}
	return l, errs
}

// ResolveWallpaper returns the wallpaper image path to use: $KITTYTK_WALLPAPER
// if set (env wins, as everywhere else here), else the ini's wallpaper,
// else empty for the built-in pattern.
func (c Config) ResolveWallpaper() string {
	if p := os.Getenv(WallpaperEnv); p != "" {
		return p
	}
	return c.Wallpaper
}

// ResolveEndpoint returns the endpoint to serve on: $KITTYTK_DISPLAY if
// set (env wins), else the ini's endpoint, else the conventional default.
func (c Config) ResolveEndpoint() string {
	if os.Getenv(client.DisplayEnv) != "" {
		return client.DefaultEndpoint() // honors the env var itself
	}
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return client.DefaultEndpoint()
}

// ResolveToken returns the shared secret: $KITTYTK_TOKEN if set (env
// wins), else the ini's token.
func (c Config) ResolveToken() string {
	if t := os.Getenv(client.TokenEnv); t != "" {
		return t
	}
	return c.Token
}

// resolveNative maps a native setting against the host OS: "mac" forces
// macOS-native shortcut glyphs on any OS, "true" enables them only when the
// host OS is macOS, and any other value keeps the default compact notation.
func resolveNative(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mac":
		return true
	case "true":
		return runtime.GOOS == "darwin"
	default:
		return false
	}
}

// UseMacNativeShortcuts resolves the [system] native setting for the graphical
// host. See resolveNative for the value semantics.
func (c Config) UseMacNativeShortcuts() bool { return resolveNative(c.Native) }

// UseTUIMacNativeShortcuts resolves the [tui] native setting for the terminal
// host. See resolveNative for the value semantics.
func (c Config) UseTUIMacNativeShortcuts() bool { return resolveNative(c.TUINative) }

// UseTUIOSC52Clipboard reports whether the terminal host mirrors Copy/Cut to
// the terminal clipboard via OSC 52. On (the default) unless the [tui]
// `clipboard` key opts into an internal-only clipboard.
func (c Config) UseTUIOSC52Clipboard() bool {
	switch strings.ToLower(strings.TrimSpace(c.TUIClipboard)) {
	case "internal", "off", "none", "false", "no", "0":
		return false
	default:
		return true
	}
}

// UseTUIOSC52Paste reports whether Paste queries the terminal for its clipboard
// via OSC 52 read-back (falling back to the internal clipboard when the
// terminal stays silent). Opt-in: only the read-enabling [tui] `clipboard`
// values turn it on. Write (Copy/Cut) is implied on for those values.
func (c Config) UseTUIOSC52Paste() bool {
	switch strings.ToLower(strings.TrimSpace(c.TUIClipboard)) {
	case "osc52-paste", "osc52+paste", "osc52paste", "full", "readwrite", "read", "paste":
		return true
	default:
		return false
	}
}
