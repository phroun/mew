package main

// The material the demo puts on its shelf. It is there to be looked at -- in
// the Connections window's Storage column, and in the files themselves -- so
// what matters is that every piece is something the desktop will take and that
// the sizes are the ones they are meant to be.

import (
	"strings"
	"testing"
)

// The five types the desktop stores. A sample of any other type is refused at
// the far end, and the demo would stock a shelf that stayed empty.
var storedTypes = map[string]bool{
	"txt": true, "psl": true, "bin": true, "ini": true, "conf": true,
}

func TestEverySampleIsSomethingTheDesktopStores(t *testing.T) {
	for _, tree := range []struct {
		name    string
		samples []sample
	}{
		{"data", dataSamples()},
		{"cache", cacheSamples()},
	} {
		seen := map[string]bool{}
		for _, s := range tree.samples {
			if !storedTypes[s.typ] {
				t.Errorf("%s/%s is a %q, which the desktop does not store", tree.name, s.key, s.typ)
			}
			if strings.TrimSpace(s.key) == "" {
				t.Errorf("%s holds a sample with no key", tree.name)
			}
			if seen[s.key] {
				t.Errorf("%s/%s is stocked twice, so one write lands on the other", tree.name, s.key)
			}
			seen[s.key] = true
			if len(s.body) == 0 {
				t.Errorf("%s/%s is empty, and shows nothing to look at", tree.name, s.key)
			}
		}
		if len(tree.samples) == 0 {
			t.Errorf("%s is stocked with nothing", tree.name)
		}
	}
}

// The blob is every byte value there is, so a hex dump of it shows a ramp and
// anything that mangled a byte on the way shows up as a break in it.
func TestTheBlobIsEveryByteValue(t *testing.T) {
	got := byteRamp(3)
	if len(got) != 3*256 {
		t.Fatalf("three laps came to %d bytes", len(got))
	}
	for i, b := range got {
		if int(b) != i%256 {
			t.Fatalf("byte %d is %d, not %d", i, b, i%256)
		}
	}
}

// One sample is larger than a statement carries, because writing something in
// pieces is the part of this worth demonstrating -- and the part a bundle will
// need.
func TestSomethingOnTheShelfNeedsMoreThanOneStatement(t *testing.T) {
	var largest int
	for _, s := range append(dataSamples(), cacheSamples()...) {
		if len(s.body) > largest {
			largest = len(s.body)
		}
	}
	if largest <= storeChunk {
		t.Errorf("the largest sample is %d bytes, under the %d a statement carries, "+
			"so nothing the demo writes is ever appended", largest, storeChunk)
	}
}

// A size reads the way the Connections window reads it, so what the app says
// it wrote can be compared with what the desktop says it holds.
func TestASizeReadsTheWayTheDesktopWritesIt(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "0b"},
		{256, "256b"},
		{6144, "6.0K"},
		{2 * 1024 * 1024, "2.0M"},
	} {
		if got := byteCount(c.n); got != c.want {
			t.Errorf("%d bytes reads as %q, want %q", c.n, got, c.want)
		}
	}
}
