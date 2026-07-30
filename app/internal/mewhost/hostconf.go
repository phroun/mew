package mewhost

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/hostcfg"
)

// The mew application host takes its KittyTK launch settings from the LAUNCHING
// user's real ~/.mew/editor.conf, using the SAME sections and keys as the
// standalone KittyTK hosts' kittytk.ini - [window], [service], [system], [tui] -
// just read from mew's own config file instead of the XDG kittytk.ini. This is
// the config of the user who started mew on the actual machine, so it is read
// straight from the OS home directory (not through mew's mew:/ sandbox, which
// scopes the edited document tree, not the launcher's own settings).
//
// These sections are inert to the editor itself (mew's config reader keeps them
// but nothing consumes them), so they coexist with the ordinary [options] /
// [general] / [storage] sections in the same file. Example:
//
//	[window]           ; graphical host (mew-sdl) only
//	title        = mew
//	width        = 1200
//	height       = 800
//	scale        = 2
//	font_size    = 14
//	border_width =         ; device px, blank/0 = default
//	fps          =         ; true shows the render frame rate
//	vsync        =         ; blank keeps the default (on); false uncaps fps
//
//	[service]          ; both hosts
//	endpoint =             ; blank = default; tcp://host:port, tls://…, or a socket path
//	token    =             ; optional shared secret
//
//	[system]           ; graphical host menu-glyph style
//	native =               ; true=native on macOS, mac=force any OS, else compact ^X/M-x
//
//	[tui]              ; terminal host (cmd/mew -tags kittytk)
//	native    =            ; same knob, independent of [system]
//	clipboard =            ; osc52/system (default), osc52-paste/full, or internal/off
//
// Environment variables still win over the file (KITTYTK_DISPLAY for the
// endpoint, KITTYTK_TOKEN for the token), via hostcfg.Config's resolvers.

// LoadHostConfig returns the KittyTK launch configuration for the mew hosts,
// read from the launching user's ~/.mew/editor.conf over the built-in defaults.
// A missing file or home directory just yields the defaults.
func LoadHostConfig() hostcfg.Config {
	cfg := hostcfg.Defaults()
	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}
	path := filepath.Join(home, ".mew", "editor.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	applyHostConf(parseHostConfSections(data), &cfg)
	cfg.Source = path
	return cfg
}

// parseHostConfSections reads editor.conf into a section -> key -> value map
// (section and key names lowercased; matching is case-insensitive), following
// mew's INI dialect closely enough for scalar settings: '#'/';' full-line and
// inline comments, [section] headers, key = value, and optional surrounding
// quotes on the value.
func parseHostConfSections(data []byte) map[string]map[string]string {
	out := map[string]map[string]string{}
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = strings.ToLower(strings.TrimSpace(line[1:end]))
			}
			continue
		}
		if section == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := trimQuotes(strings.TrimSpace(stripInlineComment(line[eq+1:])))
		if out[section] == nil {
			out[section] = map[string]string{}
		}
		out[section][key] = val
	}
	return out
}

// applyHostConf maps the [window]/[service]/[system]/[tui] keys onto the config,
// mirroring hostcfg.ini semantics: string settings apply only when non-empty (a
// blank keeps the default), numbers that don't parse are skipped, vsync is
// default-on, and native is section-routed ([system] vs [tui]).
func applyHostConf(sec map[string]map[string]string, cfg *hostcfg.Config) {
	window := sec["window"]
	service := sec["service"]
	system := sec["system"]
	tui := sec["tui"]

	// [window] - graphical host geometry and rendering.
	setStr(window, "title", &cfg.Title)
	setPos(window, "width", &cfg.Width)
	setPos(window, "height", &cfg.Height)
	setPos(window, "scale", &cfg.Scale)
	setPos(window, "font_size", &cfg.FontSize)
	// border_width accepts 0 (an explicit "no border"), unlike the others.
	if v, ok := window["border_width"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.BorderWidth = n
		}
	}
	if v, ok := window["fps"]; ok {
		cfg.ShowFPS = truthy(v)
	}
	// vsync is default-on: only an explicit falsey value disables it.
	if v, ok := window["vsync"]; ok {
		cfg.VSync = !falsey(v)
	}

	// [service] - the display endpoint and shared secret (both hosts).
	setStr(service, "endpoint", &cfg.Endpoint)
	setStr(service, "token", &cfg.Token)

	// native is section-routed: [system] styles the graphical host's menu
	// glyphs, [tui] the terminal host's; clipboard is [tui] only.
	setStr(system, "native", &cfg.Native)
	setStr(tui, "native", &cfg.TUINative)
	setStr(tui, "clipboard", &cfg.TUIClipboard)
}

// setStr applies a string key only when present and non-empty, so a blank value
// keeps the default (an empty title, in particular, would be wrong).
func setStr(sec map[string]string, key string, dst *string) {
	if v, ok := sec[key]; ok && v != "" {
		*dst = v
	}
}

// setPos applies a positive-integer key, skipping blank or unparsable values.
func setPos(sec map[string]string, key string, dst *int) {
	if v, ok := sec[key]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			*dst = n
		}
	}
}

// trimQuotes removes a single matching pair of surrounding quotes.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripInlineComment removes a trailing '#'/';' comment that begins the value or
// follows whitespace, leaving markers embedded in a token intact.
func stripInlineComment(v string) string {
	for i := 0; i < len(v); i++ {
		if c := v[i]; c == ';' || c == '#' {
			if i == 0 || v[i-1] == ' ' || v[i-1] == '\t' {
				return strings.TrimRight(v[:i], " \t")
			}
		}
	}
	return v
}

// truthy reports a permissive boolean: true/1/yes/on are true, else false.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// falsey reports whether a value explicitly disables a default-on flag. Blank is
// NOT falsey, so an empty value keeps the default.
func falsey(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no", "off":
		return true
	default:
		return false
	}
}
