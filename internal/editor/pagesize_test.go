package editor

import "testing"

func TestPageSizeEval(t *testing.T) {
	cases := []struct {
		optimal string
		overlap string
		step    int
		height  int
		want    int
	}{
		{"100%", "1", 0, 20, 19}, // default-ish: full page, one overlap
		{"100%", "0", 0, 20, 20}, // no overlap: full page (cap is height)
		{"100%", "2", 0, 20, 18}, // two overlap lines
		{"50%", "0", 0, 40, 20},  // half
		{"24", "0", 0, 50, 24},   // fixed
		{"24", "0", 0, 10, 10},   // fixed capped to height (overlap 0)
		{"24", "2", 0, 10, 8},    // fixed capped to height-overlap
		{"100%", "5", 0, 3, 1},   // overlap larger than window: still moves 1
		// percentage overlap, CEILed so it never vanishes on a short screen:
		{"100%", "10%", 0, 20, 18}, // ceil(2.0)=2 -> cap 18
		{"100%", "10%", 0, 15, 13}, // ceil(1.5)=2 -> cap 13
		{"100%", "10%", 0, 5, 4},   // ceil(0.5)=1 -> cap 4 (does not zero out)
		{"100%", "1%", 0, 20, 19},  // ceil(0.2)=1
		{"100%", "0%", 0, 20, 20},  // literal zero percent -> no overlap
		// step rounds down to a multiple:
		{"50%", "0", 6, 48, 24},
		{"50%", "0", 6, 24, 12},
		{"100%", "1", 6, 20, 18},
		{"50%", "0", 6, 8, 4}, // below one step: keep what fits
	}
	for _, c := range cases {
		spec := buildPageSizeSpec(c.optimal, c.overlap, c.step)
		if got := spec.eval(c.height); got != c.want {
			t.Errorf("optimal=%q overlap=%q step=%d @ h=%d = %d, want %d",
				c.optimal, c.overlap, c.step, c.height, got, c.want)
		}
	}
}

func TestParseCountOrPercent(t *testing.T) {
	for _, ok := range []string{"24", "50%", "0", "100%", "0%"} {
		if _, _, _, valid := parseCountOrPercent(ok); !valid {
			t.Errorf("%q should parse", ok)
		}
	}
	for _, bad := range []string{"", "abc", "50%%", "50%-1", "-4", "%"} {
		if _, _, _, valid := parseCountOrPercent(bad); valid {
			t.Errorf("%q should have failed", bad)
		}
	}
	// A malformed value falls back to defaults in buildPageSizeSpec.
	spec := buildPageSizeSpec("garbage", "junk", 0)
	if got := spec.eval(20); got != 19 { // default 100% with 1 overlap
		t.Errorf("malformed values should fall back to defaults: got %d", got)
	}
}

func TestPageOptionsLive(t *testing.T) {
	e, w := newTestEditor(t, "config\n",
		"pageSizeOptimal=50%", "pageOverlapMinimum=0", "pageSizeStep=6")
	if e.Config.PageSizeOptimal != "50%" || e.Config.PageOverlapMinimum != "0" || e.Config.PageSizeStep != 6 {
		t.Fatalf("config: %+v", e.Config)
	}
	w.ContentHeight = 48
	if _, p := e.pageSize(w); p != 24 { // 50% of 48 = 24, /6 = 24
		t.Fatalf("eval: %d, want 24", p)
	}
	// A percentage overlap, live.
	e.PawScript.ExecuteAsync("set_option pageSizeStep, 0")
	e.PawScript.ExecuteAsync("set_option pageOverlapMinimum, 25%")
	if _, p := e.pageSize(w); p != 24 { // 50% of 48 = 24, overlap ceil(12)=12, cap 36
		t.Fatalf("after overlap 25%%: %d, want 24", p)
	}
	if v, _ := e.getOption(nil, "pageOverlapMinimum"); v != "25%" {
		t.Fatalf("get_option pageOverlapMinimum: %q", v)
	}
	// Invalid overlap is rejected, spec unchanged.
	e.PawScript.ExecuteAsync("set_option pageOverlapMinimum, nonsense")
	if v, _ := e.getOption(nil, "pageOverlapMinimum"); v != "25%" {
		t.Fatalf("invalid set_option should be ignored: %q", v)
	}
}
