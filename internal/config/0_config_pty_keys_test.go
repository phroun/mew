package config

import (
	"strings"
	"testing"
)

// Backspace and Delete must reach a guest as a terminal sends them, which
// means NOT naming them in the pty capture map at all.
//
// They were named, both sending a hardcoded ^H. That is wrong twice: ^H is what
// Ctrl-H sends, where a terminal sends DEL for Backspace and CSI 3 ~ for
// forward Delete — and one byte for two keys leaves a guest unable to tell them
// apart. readline and vim forgive it because terminfo often maps erase to ^H; a
// guest that maps terminal input to real key events reads it as Ctrl-H and
// ignores it, which is how a browser in a pane ends up with no Backspace.
//
// The wildcard already encodes both correctly through the host's emulator.
func TestPTYMappingsDoNotHardcodeEraseKeys(t *testing.T) {
	cfg := (&Manager{}).generateDefaultConfig()
	i := strings.Index(cfg, "[pty::mappings]")
	if i < 0 {
		t.Fatal("no [pty::mappings] section in the default config")
	}
	section := cfg[i:]
	if j := strings.Index(section[1:], "\n["); j >= 0 {
		section = section[:j+1]
	}
	for _, bad := range []string{`del   =tinput "`, `back  =tinput "`, `\x08`} {
		if strings.Contains(section, bad) {
			t.Errorf("[pty::mappings] hardcodes an erase key (%q); the wildcard "+
				"encodes Backspace and Delete correctly on its own", bad)
		}
	}
	if !strings.Contains(section, "(capture) *     =tinput_key") {
		t.Error("the wildcard that does the encoding is gone")
	}
}
