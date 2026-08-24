package buffer

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Reading a buffer must not depend on how its content arrived. buffer_insert_file
// (^B R) builds the buffer with one large Caret.Insert; buffer_open (^B O) hands
// the content to garland at construction. Through garland v0.1.10 the insert path
// stored the whole payload in a single unbounded leaf, so every later seek that
// landed in it scanned the lot: a 200k-line file inserted that way answered
// scattered line reads 242x slower than the same file opened, and the ratio grew
// with the file. Fixed in garland v0.1.11 (tree.go, buildEditSubtree), which
// gives a large insert the same balanced shape the loader gives the same bytes.
//
// The threshold is deliberately loose. This is not a benchmark - it is a guard
// against the structural regression coming back, which was two orders of
// magnitude, not a few percent.
func TestLargeInsertReadsAsFastAsOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 50k-line buffer twice")
	}
	const n = 50000
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "line %d: the quick brown fox jumps over the lazy dog %d\n", i, i)
	}
	content := sb.String()

	opened := NewFromString(content)
	inserted := New()
	k := inserted.NewCaret()
	k.Seek(0, 0)
	k.Insert(content)

	if a, b := opened.GetLineCount(), inserted.GetLineCount(); a != b {
		t.Fatalf("line counts differ: opened=%d inserted=%d", a, b)
	}
	for _, i := range []int{0, 1, 999, n / 2, n - 1} {
		if a, b := opened.GetLine(i), inserted.GetLine(i); a != b {
			t.Fatalf("line %d differs: opened=%q inserted=%q", i, a, b)
		}
	}

	scatteredRead := func(b *Buffer) time.Duration {
		lc := b.GetLineCount()
		t0 := time.Now()
		for i := 0; i < 2000; i++ {
			b.GetLine((i * 7919) % lc)
		}
		return time.Since(t0)
	}
	// Warm both equally before measuring either.
	scatteredRead(opened)
	scatteredRead(inserted)
	ro, ri := scatteredRead(opened), scatteredRead(inserted)

	if ratio := float64(ri) / float64(ro); ratio > 5 {
		t.Errorf("inserted buffer reads %.1fx slower than the opened one "+
			"(opened=%v inserted=%v); a large insert must not build one oversized leaf",
			ratio, ro, ri)
	}
}
