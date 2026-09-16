package client

// Extend: the display saying it will HOLD the places it is sent, so a result
// may leave out what its place already carried.
//
// Nothing an author writes changes either way. They place what they have and
// send the record when they have it; the leaving-out happens in the library,
// which is the only place it can happen safely -- an author eliding by hand
// would have to know what the far end asked for.

import (
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// placesAndRecords is the same answer whichever mode it goes out in.
func placesAndRecords(f *Fill) {
	_ = f.Place(17, serval.Named("name", "src/parser.go"))
	_ = f.Place(42)
	_ = f.Placed(serval.StopExhausted, nil)
	_ = f.Record(17, serval.Named("name", "src/parser.go"), serval.Named("size", 1024))
	_ = f.Record(42, serval.Named("name", "src/window.go"))
	_ = f.Exhausted()
}

// Under extend a result carries only what its place did not -- and in the limit
// carries no fields at all, just the counts, which is the claim only a result
// can make. Whoever asked holds two fields and is now told there are two, so it
// holds the lot, without anyone deciding that.
func TestUnderExtendAResultCarriesOnlyWhatItsPlaceDidNot(t *testing.T) {
	c, r, _ := serveOne(t, placesAndRecords)
	send(t, c, `q=new query source="files" count=30 extend`)

	want := "reply q=1\n" +
		`place 1 id=17 fields={ name "src/parser.go" }` + "\n" +
		"place 1 id=42 fields={}\n" +
		"place 1 complete exhausted\n" +
		// `.name` was placed, so the result says only `.size` -- and the count,
		// which is the one thing the place could not say.
		"result 1 id=17 fields={ size 1024 } map=2\n" +
		// This one placed nothing, so its result carries the lot.
		`result 1 id=42 fields={ name "src/window.go" } map=1 complete exhausted`
	if got := strings.Join(r.since(0), "\n"); got != want {
		t.Errorf("under extend it wrote\n%s\nand should write\n%s", got, want)
	}
}

// Saying nothing is replace, and every result then carries everything: the
// places are pure decoration a reader may drop on the floor.
func TestWithoutExtendEveryResultCarriesTheLot(t *testing.T) {
	c, r, _ := serveOne(t, placesAndRecords)
	send(t, c, `q=new query source="files" count=30`)

	want := "reply q=1\n" +
		`place 1 id=17 fields={ name "src/parser.go" }` + "\n" +
		"place 1 id=42 fields={}\n" +
		"place 1 complete exhausted\n" +
		`result 1 id=17 record={ name "src/parser.go"; size 1024 }` + "\n" +
		`result 1 id=42 record={ name "src/window.go" } complete exhausted`
	if got := strings.Join(r.since(0), "\n"); got != want {
		t.Errorf("without extend it wrote\n%s\nand should write\n%s", got, want)
	}
}

// A record whose place already carried everything confirms as the counts alone,
// which is the whole of what only a result can say.
func TestAResultMayBeNothingButTheConfirmation(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Place(17, serval.Named("name", "src/parser.go"))
		_ = f.Record(17, serval.Named("name", "src/parser.go"))
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" count=30 extend`)

	want := "reply q=1\n" +
		`place 1 id=17 fields={ name "src/parser.go" }` + "\n" +
		"result 1 id=17 fields={} map=1 complete exhausted"
	if got := strings.Join(r.since(0), "\n"); got != want {
		t.Errorf("it wrote\n%s\nand should write\n%s", got, want)
	}
}
