package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// parseNumericValue: commas stripped, leading/trailing noise ignored,
// minus + digits + one decimal point honored, no digits = 0.
func TestParseNumericValue(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"2048", 2048},
		{"2,048", 2048},
		{"99 KB", 99},
		{"$1,234.56 USD", 1234.56},
		{"$-1,234.56", -1234.56},
		{"-3", -3},
		{"-.5", -0.5},
		{".5 sec", 0.5},
		{"approx 1.5x", 1.5},
		{"1.2.3", 1.2}, // second dot ends the number
		{"--", 0},
		{"", 0},
		{"Folder", 0},
	}
	for _, c := range cases {
		if got := parseNumericValue(c.in); got != c.want {
			t.Errorf("parseNumericValue(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// SetValue caches the numeric equivalent up front; NumericValue reads
// the cache (and falls back to parsing for direct Values writes).
func TestNumericValueCachedOnSet(t *testing.T) {
	it := NewTreeItem("x")
	it.SetValue("size", "1,536 KB")
	if got := it.numValues["size"]; got != 1536 {
		t.Errorf("cached numeric = %v, want 1536", got)
	}
	if got := it.NumericValue("size"); got != 1536 {
		t.Errorf("NumericValue = %v, want 1536", got)
	}
	// Direct map write (no cache): still resolves by parsing.
	it.Values["raw"] = "42 things"
	if got := it.NumericValue("raw"); got != 42 {
		t.Errorf("uncached NumericValue = %v, want 42", got)
	}
}

// A Numeric column sorts by value, not by text: "9" sorts before "10",
// which a string comparison gets backwards.
func TestTreeNumericColumnSort(t *testing.T) {
	tv := NewTreeView()
	bytes := NewTreeColumn("bytes", "Bytes", 10)
	bytes.Sortable = true
	bytes.Numeric = true
	tv.AddColumn(bytes)
	for _, spec := range []struct{ name, bytes string }{
		{"ten", "10"},
		{"nine", "9"},
		{"kilo", "1,024"},
		{"half", "0.5"},
	} {
		it := NewTreeItem(spec.name)
		it.SetValue("bytes", spec.bytes)
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})

	tv.SetSorted(true, 0, false)
	want := []string{"half", "nine", "ten", "kilo"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("numeric ascending = %v, want %v", got, want)
	}
	tv.SetSorted(true, 0, true)
	want = []string{"kilo", "ten", "nine", "half"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("numeric descending = %v, want %v", got, want)
	}
}

// SortProxy redirects the sort to another column's values while the
// indicator stays on the chosen column: a "2 KB"-style Size caption
// column sorts by a hidden numeric raw-size column.
func TestTreeSortProxyColumn(t *testing.T) {
	tv := NewTreeView()
	size := NewTreeColumn("size", "Size", 10)
	size.Sortable = true
	size.SortProxy = 1 // the rawsize column below
	tv.AddColumn(size)
	raw := NewTreeColumn("rawsize", "Raw Size", 10)
	raw.Numeric = true
	raw.Hidden = true
	tv.AddColumn(raw)

	for _, spec := range []struct{ name, size, raw string }{
		{"big", "1.5 MB", "1572864"},
		{"ten", "10 KB", "10240"},
		{"two", "2 KB", "2048"},
		{"nine", "9 KB", "9216"},
	} {
		it := NewTreeItem(spec.name)
		it.SetValue("size", spec.size)
		it.SetValue("rawsize", spec.raw)
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})

	// Sorting BY the size column (index 0) must order by the raw byte
	// counts; a string sort on the captions would give 1.5 MB first
	// and 10 KB before 2 KB.
	tv.SetSorted(true, 0, false)
	want := []string{"two", "nine", "ten", "big"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("proxied ascending = %v, want %v", got, want)
	}
	tv.SetSorted(true, 0, true)
	want = []string{"big", "ten", "nine", "two"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("proxied descending = %v, want %v", got, want)
	}

	// The indicator stays on the chosen column, not the proxy target.
	if _, by, _ := tv.Sorted(); by != 0 {
		t.Errorf("sortedBy = %d, want 0 (the visible Size column)", by)
	}

	// A self- or out-of-range proxy is ignored rather than looping.
	size.SortProxy = 0
	if idx, _ := tv.sortTarget(); idx != 0 {
		t.Errorf("self proxy resolved to %d, want 0", idx)
	}
	size.SortProxy = 99
	if idx, _ := tv.sortTarget(); idx != 0 {
		t.Errorf("out-of-range proxy resolved to %d, want 0", idx)
	}
}
