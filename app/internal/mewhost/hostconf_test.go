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
