package hostcfg

import (
	"os"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// shippedInis are the kittytk.ini files that carry the default keymap written
// out and commented out, as a reference for what is bound.
var shippedInis = []string{"../dist/kittytk.ini", "../cmd/kittytk-sdl/kittytk.ini"}

// A shipped ini binds nothing. Its [mappings] section is entirely commented
// out, so the toolkit's own keymap is what runs and the file is a reference
// rather than a second, competing statement of the table.
//
// That distinction is not academic. These blocks used to be live, and were a
// snapshot from before half the table existed: 53 lines against the default's
// 138, with 31 keys never mentioned. Loading one moved app_menu from F10 to F2
// and trinket_activate from Return to Space -- the two cases the default
// places deliberately -- and cmd/kittytk-sdl/kittytk.ini additionally moved
// window_mdi_next and window_mdi_prior onto C-Tab and C-S-Tab.
func TestShippedIniBindsNothing(t *testing.T) {
	for _, p := range shippedInis {
		cfg := Defaults()
		apply(readIni(t, p), &cfg)
		for _, b := range cfg.Mappings {
			t.Errorf("%s binds %q = %v; every line should be commented out", p, b.Key, b.Commands)
		}
	}
}

// ...and what it has commented out is the default keymap itself, line for line
// and in order. Uncommenting a line has to be a no-op on what the key means,
// or the file is misinforming the person reading it.
func TestShippedIniListsTheDefaultKeymap(t *testing.T) {
	want := core.ParseKeymap(core.DefaultKeymapConfig)
	for _, p := range shippedInis {
		got := core.ParseKeymap(uncommentMappings(string(readIni(t, p))))
		if len(got) != len(want) {
			t.Errorf("%s lists %d bindings, the default has %d", p, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i].Key != want[i].Key || got[i].Commands[0] != want[i].Commands[0] {
				t.Errorf("%s line %d: %q = %q, want %q = %q", p, i+1,
					got[i].Key, got[i].Commands[0], want[i].Key, want[i].Commands[0])
			}
		}
	}
}

func readIni(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

// uncommentMappings returns the file's [mappings] section with the commented
// out bindings live again. A commented binding is spelled ";key = command",
// with no space after the semicolon; prose is "; text", which stays a comment.
func uncommentMappings(s string) string {
	i := strings.Index(s, "\n[mappings]\n")
	if i < 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(s[i+1:], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") && !strings.HasPrefix(trimmed, "; ") && strings.Contains(trimmed, "=") {
			b.WriteString(strings.TrimPrefix(trimmed, ";") + "\n")
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
