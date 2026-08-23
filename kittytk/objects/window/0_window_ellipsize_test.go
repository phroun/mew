package window

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// ellipsizeToWidth binary-searches the longest prefix that fits, on the
// assumption that text never gets narrower for having one more character
// in it. Pin that against the scan-from-the-full-string it replaced: at
// every width, on the REAL shaper, the two must agree exactly.
func TestEllipsizeMatchesLinearScan(t *testing.T) {
	be, err := raster.New(800, 600)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(be)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	f := &core.Font{Name: "ui-text", Size: 12}

	linear := func(s string, avail core.Unit) string {
		const ell = "..."
		if f.MeasureText(s) <= avail {
			return s
		}
		r := []rune(s)
		for len(r) > 0 {
			r = r[:len(r)-1]
			if f.MeasureText(string(r)+ell) <= avail {
				return string(r) + ell
			}
		}
		return ""
	}

	for _, s := range []string{
		"", "x", "..", "Report", "Report Viewer",
		"A rather long window title - project/path/to/file.go",
		"iiiiiiiiiiWWWWWWWWWWiiiiiiiiii", // widely varying advances
		"日本語のタイトルです",                     // wide glyphs
	} {
		for avail := core.Unit(-4); avail <= 420; avail++ {
			if got, want := ellipsizeToWidth(s, avail, f), linear(s, avail); got != want {
				t.Fatalf("%q at avail %d: got %q, want %q", s, avail, got, want)
			}
		}
	}
}
