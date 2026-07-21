package hostcfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/client"
)

// apply parses recognized keys onto a Config, tolerates section headers
// and comments, and leaves defaults for absent/typo'd keys.
func TestApplyParsesKnownKeys(t *testing.T) {
	cfg := Defaults()
	apply([]byte(`
# a comment
; another
[window]
title = My Desk
width = 800
height = 600
scale = 1
font_size = 16
border_width = 3

[service]
endpoint = tcp://0.0.0.0:9797
token = s3cret
bogus = ignored
scale = notanumber
`), &cfg)

	if cfg.Title != "My Desk" || cfg.Width != 800 || cfg.Height != 600 || cfg.Scale != 1 {
		t.Errorf("window: %+v", cfg)
	}
	if cfg.FontSize != 16 {
		t.Errorf("font_size = %d, want 16", cfg.FontSize)
	}
	if cfg.BorderWidth != 3 {
		t.Errorf("border_width = %d, want 3", cfg.BorderWidth)
	}
	// Absent border_width keeps the default (0 = built-in hairline).
	if Defaults().BorderWidth != 0 {
		t.Errorf("default border_width = %d, want 0", Defaults().BorderWidth)
	}
	// An absent/typo'd font_size keeps the default (12).
	if Defaults().FontSize != 12 {
		t.Errorf("default font_size = %d, want 12", Defaults().FontSize)
	}
	if cfg.Endpoint != "tcp://0.0.0.0:9797" || cfg.Token != "s3cret" {
		t.Errorf("service: endpoint=%q token=%q", cfg.Endpoint, cfg.Token)
	}
}

// The [window] fps flag parses permissively and defaults to off.
func TestApplyParsesFPS(t *testing.T) {
	if Defaults().ShowFPS {
		t.Error("default ShowFPS should be false")
	}
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"true", true}, {"1", true}, {"yes", true}, {"ON", true},
		{"false", false}, {"0", false}, {"", false}, {"maybe", false},
	} {
		cfg := Defaults()
		apply([]byte("[window]\nfps = "+tc.val+"\n"), &cfg)
		if cfg.ShowFPS != tc.want {
			t.Errorf("fps=%q -> ShowFPS=%v, want %v", tc.val, cfg.ShowFPS, tc.want)
		}
	}
}

// vsync is a default-on flag: absent or blank stays true, only an explicit
// falsey value turns it off.
func TestApplyParsesVSync(t *testing.T) {
	if !Defaults().VSync {
		t.Error("default VSync should be true")
	}
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"false", false}, {"0", false}, {"no", false}, {"OFF", false},
		{"true", true}, {"1", true}, {"", true}, {"whatever", true},
	} {
		cfg := Defaults()
		apply([]byte("[window]\nvsync = "+tc.val+"\n"), &cfg)
		if cfg.VSync != tc.want {
			t.Errorf("vsync=%q -> VSync=%v, want %v", tc.val, cfg.VSync, tc.want)
		}
	}
	// Absent entirely keeps the default.
	cfg := Defaults()
	apply([]byte("[window]\ntitle = x\n"), &cfg)
	if !cfg.VSync {
		t.Error("absent vsync should keep default true")
	}
}

// Inline (trailing) comments are stripped from values, including the
// "blank value + explanatory comment" case that should yield an empty
// endpoint - but a ';'/'#' inside a value with no leading space is kept.
func TestApplyStripsInlineComments(t *testing.T) {
	cfg := Defaults()
	apply([]byte(`
endpoint =      ; blank = default; tcp://…, or a socket path
scale = 1   # crisp
title = My # Desk
token = a;b#c
`), &cfg)

	if cfg.Endpoint != "" {
		t.Errorf("endpoint should be empty (comment stripped), got %q", cfg.Endpoint)
	}
	if cfg.Scale != 1 {
		t.Errorf("scale with trailing comment = %d, want 1", cfg.Scale)
	}
	if cfg.Title != "My" {
		t.Errorf("title = %q, want %q (space+# starts a comment)", cfg.Title, "My")
	}
	if cfg.Token != "a;b#c" {
		t.Errorf("token = %q, want %q (no leading space -> not a comment)", cfg.Token, "a;b#c")
	}
}

// CRLF line endings (as a Windows user's Notepad would save) parse the
// same as LF: the trailing \r is not part of the value.
func TestApplyHandlesCRLF(t *testing.T) {
	cfg := Defaults()
	apply([]byte("title = Win\r\nscale = 3\r\nendpoint = tcp://127.0.0.1:9797\r\n"), &cfg)
	if cfg.Title != "Win" || cfg.Scale != 3 || cfg.Endpoint != "tcp://127.0.0.1:9797" {
		t.Errorf("CRLF parse: %+v", cfg)
	}
}

// A malformed number leaves the default rather than zeroing the field.
func TestApplyKeepsDefaultOnBadNumber(t *testing.T) {
	cfg := Defaults()
	apply([]byte("scale = oops\nwidth = -5\n"), &cfg)
	if cfg.Scale != Defaults().Scale || cfg.Width != Defaults().Width {
		t.Errorf("bad numbers should keep defaults: scale=%d width=%d", cfg.Scale, cfg.Width)
	}
}

// Section headers are optional: keys are matched by name.
func TestApplyIgnoresSections(t *testing.T) {
	cfg := Defaults()
	apply([]byte("title = No Sections Here\nscale = 3\n"), &cfg)
	if cfg.Title != "No Sections Here" || cfg.Scale != 3 {
		t.Errorf("sectionless keys should apply: %+v", cfg)
	}
}

// The [tui] clipboard key toggles OSC 52 clipboard integration. It defaults
// on, and only [tui] carries it (like native, it is section-sensitive).
func TestTUIClipboardConfig(t *testing.T) {
	// Default (no key): OSC 52 on.
	if def := Defaults(); !def.UseTUIOSC52Clipboard() {
		t.Error("default should mirror to OSC 52")
	}

	// [tui] clipboard=internal disables it.
	cfg := Defaults()
	apply([]byte("[tui]\nclipboard = internal\n"), &cfg)
	if cfg.UseTUIOSC52Clipboard() {
		t.Error("clipboard=internal should disable OSC 52")
	}

	// An explicit on-value keeps it enabled.
	cfg = Defaults()
	apply([]byte("[tui]\nclipboard = osc52\n"), &cfg)
	if !cfg.UseTUIOSC52Clipboard() {
		t.Error("clipboard=osc52 should enable OSC 52")
	}

	// Read-back is opt-in: only the paste-enabling values turn it on, and they
	// keep write on too.
	if Defaults().UseTUIOSC52Paste() {
		t.Error("default should not enable OSC 52 read-back")
	}
	cfg = Defaults()
	apply([]byte("[tui]\nclipboard = osc52\n"), &cfg)
	if cfg.UseTUIOSC52Paste() {
		t.Error("clipboard=osc52 is write-only, read-back should be off")
	}
	cfg = Defaults()
	apply([]byte("[tui]\nclipboard = osc52-paste\n"), &cfg)
	if !cfg.UseTUIOSC52Paste() || !cfg.UseTUIOSC52Clipboard() {
		t.Error("clipboard=osc52-paste should enable both read-back and write")
	}

	// The key is section-sensitive: outside [tui] it does not bind.
	cfg = Defaults()
	apply([]byte("clipboard = internal\n"), &cfg)
	if cfg.TUIClipboard != "" {
		t.Errorf("clipboard outside [tui] should not bind, got %q", cfg.TUIClipboard)
	}
}

// The [fonts] section is a family -> path map (keys keep their case); [window]
// fonts_path and ui_term are comma-separated lists.
func TestApplyFontConfig(t *testing.T) {
	cfg := Defaults()
	apply([]byte(`
[fonts]
JetBrainsMono = /usr/share/fonts/jbm.ttf
Comic Mono = "/opt/comic mono.otf"

[window]
fonts_path = /a/fonts, "/b/more fonts"
ui_term = "JetBrainsMono, Monday"
ui_text_hebrew_serif = SBL Hebrew
`), &cfg)

	if cfg.Fonts["JetBrainsMono"] != "/usr/share/fonts/jbm.ttf" {
		t.Errorf("Fonts[JetBrainsMono] = %q", cfg.Fonts["JetBrainsMono"])
	}
	if cfg.Fonts["Comic Mono"] != "/opt/comic mono.otf" {
		t.Errorf("Fonts[Comic Mono] = %q (quotes should strip)", cfg.Fonts["Comic Mono"])
	}
	if len(cfg.FontsPath) != 2 || cfg.FontsPath[0] != "/a/fonts" || cfg.FontsPath[1] != "/b/more fonts" {
		t.Errorf("FontsPath = %v", cfg.FontsPath)
	}
	if got := cfg.FontAliases["ui-term"]; len(got) != 2 || got[0] != "JetBrainsMono" || got[1] != "Monday" {
		t.Errorf("FontAliases[ui-term] = %v", got)
	}
	if got := cfg.FontAliases["ui-text-hebrew-serif"]; len(got) != 1 || got[0] != "SBL Hebrew" {
		t.Errorf("FontAliases[ui-text-hebrew-serif] = %v", got)
	}
}

// Load resolves relative [fonts] paths and fonts_path against the ini's own
// directory, so fonts can ship next to kittytk.ini.
func TestLoadResolvesRelativeFontPaths(t *testing.T) {
	dir := t.TempDir()
	ini := "[fonts]\nMyFont = fonts/my.ttf\n\n[window]\nfonts_path = extra\n"
	if err := os.WriteFile(filepath.Join(dir, IniName), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg := Load()
	if got, want := cfg.Fonts["MyFont"], filepath.Join(dir, "fonts/my.ttf"); got != want {
		t.Errorf("Fonts[MyFont] = %q, want %q", got, want)
	}
	if len(cfg.FontsPath) != 1 || cfg.FontsPath[0] != filepath.Join(dir, "extra") {
		t.Errorf("FontsPath = %v, want [%s]", cfg.FontsPath, filepath.Join(dir, "extra"))
	}
}

// [tui] pseudofont_<group> = off disables a by-name cipher; fraktur_mode is a
// separate VT-request knob. Both are read only under [tui].
func TestApplyTUIPseudoFonts(t *testing.T) {
	cfg := Defaults()
	apply([]byte(`
[tui]
pseudofont_fraktur = off
pseudofont_double = no
pseudofont_black_serif = on
fraktur_mode = native
`), &cfg)

	if !cfg.TUIPseudoFontsDisabled["fraktur"] {
		t.Errorf("pseudofont_fraktur=off should disable the fraktur cipher")
	}
	if !cfg.TUIPseudoFontsDisabled["double"] {
		t.Errorf("pseudofont_double=no should disable double")
	}
	if cfg.TUIPseudoFontsDisabled["black_serif"] {
		t.Errorf("pseudofont_black_serif=on should stay enabled")
	}
	if cfg.TUIFrakturMode != "native" {
		t.Errorf("fraktur_mode=native should set TUIFrakturMode, got %q", cfg.TUIFrakturMode)
	}

	// An unrecognized fraktur_mode is ignored (stays empty -> backend default).
	c1 := Defaults()
	apply([]byte("[tui]\nfraktur_mode = sideways\n"), &c1)
	if c1.TUIFrakturMode != "" {
		t.Errorf("invalid fraktur_mode should be ignored, got %q", c1.TUIFrakturMode)
	}

	// The same keys outside [tui] are ignored.
	c2 := Defaults()
	apply([]byte("pseudofont_fraktur = off\nfraktur_mode = native\n"), &c2)
	if len(c2.TUIPseudoFontsDisabled) != 0 || c2.TUIFrakturMode != "" {
		t.Errorf("pseudofont/fraktur_mode outside [tui] should be ignored")
	}
}

// Load uses the first kittytk.ini found; the current directory is searched
// before the exe dir and the user config dir.
func TestLoadFirstFoundWinsFromCWD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, IniName), []byte("title = FromCWD\nwidth = 640\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg := Load()
	if cfg.Title != "FromCWD" || cfg.Width != 640 {
		t.Errorf("expected CWD ini to win: %+v", cfg)
	}
	if cfg.Source != filepath.Join(dir, IniName) {
		t.Errorf("Source = %q, want the CWD ini", cfg.Source)
	}
}

// With no ini anywhere, Load returns the built-in defaults.
func TestLoadDefaultsWhenNoIni(t *testing.T) {
	empty := t.TempDir()
	t.Chdir(empty)
	// Point the user config dir at another empty dir so no stray ini is found.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	cfg := Load()
	d := Defaults()
	// Config carries maps/slices (fonts) and so is no longer comparable with
	// ==; check the scalar knobs field-wise and assert no font config appeared.
	if cfg.Title != d.Title || cfg.Width != d.Width || cfg.Height != d.Height ||
		cfg.Scale != d.Scale || cfg.FontSize != d.FontSize || cfg.BorderWidth != d.BorderWidth ||
		cfg.ShowFPS != d.ShowFPS || cfg.VSync != d.VSync || cfg.Endpoint != d.Endpoint ||
		cfg.Token != d.Token || cfg.Native != d.Native || cfg.TUINative != d.TUINative ||
		cfg.TUIClipboard != d.TUIClipboard {
		t.Errorf("no ini should yield defaults, got %+v", cfg)
	}
	if len(cfg.Fonts) != 0 || len(cfg.FontsPath) != 0 || len(cfg.FontAliases) != 0 {
		t.Errorf("no ini should yield no font config, got %+v", cfg)
	}
}

// Environment variables win over the ini for endpoint and token.
func TestResolveEnvOverrides(t *testing.T) {
	cfg := Config{Endpoint: "tcp://ini:1", Token: "initoken"}

	t.Setenv(client.DisplayEnv, "tcp://env:2")
	if got := cfg.ResolveEndpoint(); got != "tcp://env:2" {
		t.Errorf("endpoint env should win: %q", got)
	}
	t.Setenv(client.TokenEnv, "envtoken")
	if got := cfg.ResolveToken(); got != "envtoken" {
		t.Errorf("token env should win: %q", got)
	}
}

// Without the env vars, the ini's values are used (and blank endpoint
// falls back to the conventional default).
func TestResolveFallsBackToIniAndDefault(t *testing.T) {
	t.Setenv(client.DisplayEnv, "")
	t.Setenv(client.TokenEnv, "")

	ini := Config{Endpoint: "tcp://ini:1", Token: "initoken"}
	if got := ini.ResolveEndpoint(); got != "tcp://ini:1" {
		t.Errorf("ini endpoint should be used: %q", got)
	}
	if got := ini.ResolveToken(); got != "initoken" {
		t.Errorf("ini token should be used: %q", got)
	}

	blank := Config{}
	if got := blank.ResolveEndpoint(); got != client.DefaultEndpoint() {
		t.Errorf("blank endpoint should fall back to default: %q", got)
	}
}

// native is section-sensitive: [system] configures the graphical host and
// [tui] the terminal host, independently, while every other key stays matched
// by name regardless of section.
func TestApplyRoutesNativeBySection(t *testing.T) {
	cfg := Defaults()
	apply([]byte(`
[system]
native = mac
[tui]
native = true
`), &cfg)
	if cfg.Native != "mac" {
		t.Errorf("[system] native = %q, want %q", cfg.Native, "mac")
	}
	if cfg.TUINative != "true" {
		t.Errorf("[tui] native = %q, want %q", cfg.TUINative, "true")
	}
}

// A bare native key (no section) applies to the graphical host, preserving the
// section-cosmetic default for the common case.
func TestApplyNativeNoSectionIsSystem(t *testing.T) {
	cfg := Defaults()
	apply([]byte("native = mac\n"), &cfg)
	if cfg.Native != "mac" {
		t.Errorf("bare native = %q, want %q on Native", cfg.Native, "mac")
	}
	if cfg.TUINative != "" {
		t.Errorf("bare native should not set TUINative, got %q", cfg.TUINative)
	}
}

// resolveNative: "mac" forces on, unknown/blank forces off, on any OS.
func TestResolveNativeValues(t *testing.T) {
	if !resolveNative("mac") {
		t.Error(`"mac" should force native on any OS`)
	}
	if !resolveNative("  MAC ") {
		t.Error(`"mac" should be case- and space-insensitive`)
	}
	for _, v := range []string{"", "false", "no", "1", "yes"} {
		if resolveNative(v) {
			t.Errorf("resolveNative(%q) should be false", v)
		}
	}
}
