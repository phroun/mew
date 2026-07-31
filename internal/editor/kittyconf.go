package editor

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Kitty's bidi is render-only: with force_ltr off (its default) it reorders RTL
// text at the pixel stage while keeping its line buffer in logical order, and
// with force_ltr on it leaves logical order alone. Nothing over the wire reveals
// which way it is set — CPR reports the logical cursor column either way, and
// get-text / any rectangular checksum reads the logical buffer — so the config
// file is the only reliable signal for whether mew must flip its RTL emission
// for the host (force_ltr off) or own the bidi and NOT flip (force_ltr on).

// kittyForceLTR reads the user's kitty.conf and reports the effective force_ltr
// value (last assignment wins; `include` directives are followed). found is
// false when no kitty.conf could be read, so the caller keeps its own default.
func kittyForceLTR() (forceLTR bool, found bool) {
	dir := kittyConfigDir()
	if dir == "" {
		return false, false
	}
	return scanKittyForceLTR(filepath.Join(dir, "kitty.conf"), dir, 0)
}

// kittyConfigDir resolves the directory Kitty loads kitty.conf from, mirroring
// Kitty's own precedence: $KITTY_CONFIG_DIRECTORY, else $XDG_CONFIG_HOME/kitty,
// else ~/.config/kitty (the default on both Linux and macOS).
func kittyConfigDir() string {
	if d := os.Getenv("KITTY_CONFIG_DIRECTORY"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "kitty")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "kitty")
}

// scanKittyForceLTR parses one kitty.conf, following `include` directives up to
// a small depth (kitty applies includes inline, in file order). It returns the
// last force_ltr value seen and whether any file was read. Glob/env include
// forms are handled best-effort as plain paths; if they don't resolve they are
// simply skipped, leaving the outer file's value in force.
func scanKittyForceLTR(path, dir string, depth int) (forceLTR bool, found bool) {
	if depth > 5 {
		return false, false
	}
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()

	found = true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "force_ltr":
			forceLTR = kittyBool(fields[1])
		case "include", "globinclude", "envinclude":
			inc := fields[1]
			if !filepath.IsAbs(inc) {
				inc = filepath.Join(dir, inc)
			}
			// An include applies its options where it sits, so its force_ltr (if
			// any) overrides what came before; a later assignment in this file
			// still wins over the include, preserving last-wins order.
			if v, ok := scanKittyForceLTR(inc, filepath.Dir(inc), depth+1); ok {
				forceLTR = v
			}
		}
	}
	return forceLTR, found
}

// kittyBool parses a kitty.conf boolean (yes/no, with the usual synonyms).
func kittyBool(s string) bool {
	switch strings.ToLower(s) {
	case "yes", "y", "true", "1":
		return true
	}
	return false
}
