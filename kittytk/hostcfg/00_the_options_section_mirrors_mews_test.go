package hostcfg

import "testing"

// [options] mirrors mew's section of the same name, for the two knobs that mean
// the same thing on either side of the wire. Both spellings are taken: mew
// writes rtlMarkMode, and this file's own keys are underscored, so a reader has
// whichever one they already had in mind.
func TestTheOptionsSectionMirrorsMews(t *testing.T) {
	for _, c := range []struct {
		name, body string
		mode       string
		combining  bool
		said       bool
	}{
		{"mew's spelling", "[options]\nrtlMarkMode=compose\nrtlCombining=false\n",
			"compose", false, true},
		{"this file's own", "[options]\nrtl_mark_mode=compose\nrtl_combining=no\n",
			"compose", false, true},
		{"shown says so too", "[options]\nrtlCombining=yes\n", "", true, true},
		{"unsaid is left alone", "[options]\nrtlMarkMode=compose\n",
			"compose", false, false},
	} {
		cfg := Defaults()
		apply([]byte(c.body), &cfg)
		if cfg.RtlMarkMode != c.mode {
			t.Errorf("%s: rtlMarkMode = %q, want %q", c.name, cfg.RtlMarkMode, c.mode)
		}
		switch {
		case c.said && cfg.RtlCombining == nil:
			t.Errorf("%s: rtlCombining was not read", c.name)
		case c.said && *cfg.RtlCombining != c.combining:
			t.Errorf("%s: rtlCombining = %v, want %v",
				c.name, *cfg.RtlCombining, c.combining)
		case !c.said && cfg.RtlCombining != nil:
			t.Errorf("%s: rtlCombining was set from a file that never said it", c.name)
		}
	}
}
