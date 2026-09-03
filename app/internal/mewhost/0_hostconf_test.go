package mewhost

import (
	"testing"

	"github.com/phroun/kittytk/hostcfg"
)

// The [window]/[service]/[system]/[tui] sections of editor.conf map onto the
// KittyTK launch config exactly as kittytk.ini does, including the section
// routing of `native` and the default-on `vsync`.
func TestApplyHostConfSections(t *testing.T) {
	const conf = `
[options]
tabSize = 4

[window]
title       = mew rocks
width       = 1200
height      = 800
scale       = 3
font_size   = 14
border_width = 2
fps         = true
vsync       = false

[service]
endpoint = tls://localhost:9000
token    = s3cr3t

[system]
native = mac

[tui]
native    = true
clipboard = internal
`
	sec := parseHostConfSections([]byte(conf))
	cfg := hostcfg.Defaults()
	applyHostConf(sec, &cfg)

	if cfg.Title != "mew rocks" {
		t.Errorf("title = %q", cfg.Title)
	}
	if cfg.Width != 1200 || cfg.Height != 800 || cfg.Scale != 3 || cfg.FontSize != 14 {
		t.Errorf("geometry = %dx%d scale=%d font=%d", cfg.Width, cfg.Height, cfg.Scale, cfg.FontSize)
	}
	if cfg.BorderWidth != 2 {
		t.Errorf("border_width = %d", cfg.BorderWidth)
	}
	if !cfg.ShowFPS {
		t.Error("fps should be on")
	}
	if cfg.VSync {
		t.Error("vsync=false should disable it")
	}
	if cfg.Endpoint != "tls://localhost:9000" || cfg.Token != "s3cr3t" {
		t.Errorf("service endpoint=%q token=%q", cfg.Endpoint, cfg.Token)
	}
	// native is section-routed: [system] -> graphical, [tui] -> terminal.
	if cfg.Native != "mac" {
		t.Errorf("[system] native = %q, want mac", cfg.Native)
	}
	if cfg.TUINative != "true" {
		t.Errorf("[tui] native = %q, want true", cfg.TUINative)
	}
	if cfg.TUIClipboard != "internal" {
		t.Errorf("[tui] clipboard = %q, want internal", cfg.TUIClipboard)
	}
}

// A blank value keeps the default (notably title and vsync), and unrelated
// sections are ignored.
func TestApplyHostConfBlanksKeepDefaults(t *testing.T) {
	const conf = `
[window]
title =
vsync =

[storage]
backups = ~/somewhere
`
	sec := parseHostConfSections([]byte(conf))
	cfg := hostcfg.Defaults()
	applyHostConf(sec, &cfg)

	if cfg.Title != "KittyTK" {
		t.Errorf("blank title should keep default, got %q", cfg.Title)
	}
	if !cfg.VSync {
		t.Error("blank vsync should keep the default (on)")
	}
}

// Inline comments and quoted values parse the way the rest of editor.conf does.
func TestParseHostConfCommentsAndQuotes(t *testing.T) {
	const conf = `
[window]
title = "my editor"   # trailing comment
scale = 2 ; also a comment
`
	sec := parseHostConfSections([]byte(conf))
	if got := sec["window"]["title"]; got != "my editor" {
		t.Errorf("title = %q, want unquoted 'my editor'", got)
	}
	if got := sec["window"]["scale"]; got != "2" {
		t.Errorf("scale = %q, want 2 (comment stripped)", got)
	}
}

// editor.conf reads titlebar_scale with the same rules as kittytk.ini: a
// positive float applies, anything else keeps the classic 1.0.
func TestApplyHostConfTitleBarScale(t *testing.T) {
	for _, c := range []struct {
		val  string
		want float64
	}{
		{"0.7", 0.7},
		{"0", 1},
		{"-2", 1},
		{"nope", 1},
	} {
		sec := parseHostConfSections([]byte("[window]\ntitlebar_scale = " + c.val + "\n"))
		cfg := hostcfg.Defaults()
		applyHostConf(sec, &cfg)
		if cfg.TitleBarScale != c.want {
			t.Errorf("titlebar_scale = %q: got %v, want %v", c.val, cfg.TitleBarScale, c.want)
		}
	}
	// An absent key keeps the default.
	cfg := hostcfg.Defaults()
	applyHostConf(parseHostConfSections([]byte("[window]\nwidth = 900\n")), &cfg)
	if cfg.TitleBarScale != 1 {
		t.Errorf("absent titlebar_scale = %v, want 1", cfg.TitleBarScale)
	}
}

// editor.conf reads menu_scale on titlebar_scale's terms.
//
// Per-parser for the reason the density test states: mew maps editor.conf's
// keys itself, so menu_scale existing on hostcfg.Config and in upstream's
// parser left it inert here -- which is how it shipped doing nothing, and the
// note below had already said it would.
func TestApplyHostConfMenuScale(t *testing.T) {
	for _, c := range []struct {
		val  string
		want float64
	}{
		{"0.9", 0.9},
		{"0.5", 0.5},
		{"0", 1},
		{"-2", 1},
		{"nope", 1},
	} {
		sec := parseHostConfSections([]byte("[window]\nmenu_scale = " + c.val + "\n"))
		cfg := hostcfg.Defaults()
		applyHostConf(sec, &cfg)
		if cfg.MenuScale != c.want {
			t.Errorf("menu_scale = %q: got %v, want %v", c.val, cfg.MenuScale, c.want)
		}
	}
	// An absent key keeps the default, and the two scales are independent.
	cfg := hostcfg.Defaults()
	applyHostConf(parseHostConfSections([]byte("[window]\ntitlebar_scale = 0.7\n")), &cfg)
	if cfg.MenuScale != 1 {
		t.Errorf("titlebar_scale moved menu_scale to %v", cfg.MenuScale)
	}
	cfg = hostcfg.Defaults()
	applyHostConf(parseHostConfSections([]byte("[window]\nmenu_scale = 0.9\n")), &cfg)
	if cfg.MenuScale != 0.9 || cfg.TitleBarScale != 1 {
		t.Errorf("menu_scale 0.9 gave menu %v / titlebar %v, want 0.9 and 1",
			cfg.MenuScale, cfg.TitleBarScale)
	}
}

// editor.conf reads the two shortcut scales, on menu_scale's terms.
//
// Per-parser for the reason the density test states: mew maps editor.conf's
// keys itself, so a key that exists on hostcfg.Config and in upstream's
// parser is inert here until it is mapped.
func TestApplyHostConfShortcutScales(t *testing.T) {
	for _, c := range []struct {
		val  string
		want float64
	}{
		{"0.5", 0.5},
		{"1", 1},
		{"0", 0.8},
		{"-2", 0.8},
		{"nope", 0.8},
	} {
		cfg := hostcfg.Defaults()
		applyHostConf(parseHostConfSections([]byte("[window]\nshortcut_scale = "+c.val+"\n")), &cfg)
		if cfg.ShortcutScale != c.want {
			t.Errorf("shortcut_scale = %q: got %v, want %v", c.val, cfg.ShortcutScale, c.want)
		}
		cfg = hostcfg.Defaults()
		applyHostConf(parseHostConfSections([]byte("[window]\nshortcut_native_scale = "+c.val+"\n")), &cfg)
		if cfg.ShortcutNativeScale != c.want {
			t.Errorf("shortcut_native_scale = %q: got %v, want %v", c.val, cfg.ShortcutNativeScale, c.want)
		}
	}
	// Absent keys keep the defaults, and the two are independent.
	cfg := hostcfg.Defaults()
	applyHostConf(parseHostConfSections([]byte("[window]\nshortcut_scale = 0.5\n")), &cfg)
	if cfg.ShortcutScale != 0.5 || cfg.ShortcutNativeScale != 0.8 {
		t.Errorf("got %v / %v, want 0.5 / 0.8", cfg.ShortcutScale, cfg.ShortcutNativeScale)
	}
}

// [system] density overrides what the window system reports about the screen.
//
// mew reads editor.conf with ITS OWN key mapping, so a key added to the shared
// Config and to upstream's parser is still inert here until it is mapped — which
// is exactly how this one shipped doing nothing. The test is per-parser for that
// reason.
func TestApplyHostConfDensity(t *testing.T) {
	for _, c := range []struct {
		val  string
		want float64
	}{
		{"2", 2},
		{"1.5", 1.5},
		{"0", 0},    // auto
		{"-2", 0},   // auto: a negative density is not a density
		{"nope", 0}, // auto, rather than pinning a wrong number
		{"", 0},     // auto
	} {
		sec := parseHostConfSections([]byte("[system]\ndensity = " + c.val + "\n"))
		cfg := hostcfg.Defaults()
		applyHostConf(sec, &cfg)
		if cfg.Density != c.want {
			t.Errorf("density = %q: got %v, want %v", c.val, cfg.Density, c.want)
		}
	}
	// Absent leaves it on auto, and it is INDEPENDENT of scale - the one
	// combination that proves the two are different quantities is a user who
	// wants real pixels on a HiDPI panel.
	cfg := hostcfg.Defaults()
	applyHostConf(parseHostConfSections([]byte("[window]\nscale = 1\n\n[system]\ndensity = 2\n")), &cfg)
	if cfg.Scale != 1 || cfg.Density != 2 {
		t.Errorf("scale=%d density=%v, want scale 1 with density 2", cfg.Scale, cfg.Density)
	}
}
