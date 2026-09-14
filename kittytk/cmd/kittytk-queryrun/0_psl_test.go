package main

// Driving a PSL source from a query file.
//
// The statements are the ones a display would have sent, and what comes back is
// printed by the same code that prints what an application sent -- so the test
// is that the file opens, refills, restates and lets go, and that the answer
// arrives as the wire language either way.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/phroun/kittytk/source"
	"github.com/phroun/kittytk/wire"
)

const objects = `(
  ("README.md", size: 2048),
  ("build.sh", size: 310),
  ("go.mod", size: 96),
  ("src/parser.go", size: 14022),
  notes: "a bare string"
)`

// drive runs a query file against a PSL source and gives back what was printed.
func drive(t *testing.T, reading source.Reading, query string) *printer {
	t.Helper()
	src, err := source.ParsePSLSource(objects, reading)
	if err != nil {
		t.Fatal(err)
	}
	script, err := wire.Parse(query)
	if err != nil {
		t.Fatal(err)
	}
	out := &printer{}
	if err := run(src, script, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAQueryFileIsAnsweredOutOfAPSLFile(t *testing.T) {
	p := drive(t, source.Whole,
		`q=new query source="objects" filter={ ge .size 1000 } sort={ .size desc } count=2`)

	if strings.Join(p.columns, ",") != "key,.0,.size" {
		t.Errorf("the columns are %v", p.columns)
	}
	if len(p.rows) != 2 {
		t.Fatalf("%d rows", len(p.rows))
	}
	if strings.Join(p.rows[0], "|") != `3|"src/parser.go"|14022` {
		t.Errorf("the first row is %v", p.rows[0])
	}
	// Two records are the whole of what the filter holds, so the scope ran out
	// of sequence rather than out of room.
	if !strings.Contains(p.note, "every record there is") {
		t.Errorf("what ended the scope reads %q", p.note)
	}
}

// A query states its sequence and asks for one scope of it, and is answered
// once. So a second scope is a second query, naming the sequence again and
// saying which record to carry on past -- and letting the first one go is a
// statement of its own.
func TestAFileOpensRefillsAndLetsGo(t *testing.T) {
	p := drive(t, source.Whole, strings.Join([]string{
		`q=new query source="objects" sort={ .size } count=2`,
		`n=new query source="objects" sort={ .size } after=1 count=1`,
		`r=new query source="objects" sort={ .size desc } count=1`,
		`destroy q`,
	}, "\n"))

	var keys []string
	for _, row := range p.rows {
		keys = append(keys, row[0])
	}
	// By size ascending the bare string has none and leads, then go.mod and
	// build.sh; the scope after build.sh is README.md; and the other way
	// round the first record is the largest.
	if strings.Join(keys, ",") != `"notes",2,0,3` {
		t.Errorf("the records that came back are %v", keys)
	}
}

// A query cannot be restated, so a file that tries is refused rather than
// quietly answering the wrong sequence.
func TestAFileCannotRestateAQuery(t *testing.T) {
	src, err := source.ParsePSLSource(objects, source.Whole)
	if err != nil {
		t.Fatal(err)
	}
	script, err := wire.Parse(`q=new query source="objects" count=1` + "\n" +
		`set q sort={ .size }`)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(src, script, &printer{}); err == nil {
		t.Error("a restatement was accepted")
	}
}

// The shorter reading names the members alone.
//
// The bare string matches -- under Members it has no members, so it has no
// size, and undefined sits below every number, which is what `lt` is being
// asked -- but it carries nothing, so there is no cell of it to show. Its key
// is not a column: what identifies a record is not one of its fields.
func TestTheMembersReadingNamesTheMembersAlone(t *testing.T) {
	p := drive(t, source.Members,
		`q=new query source="objects" filter={ lt size 1000 } sort={ size } count=3`)

	if strings.Join(p.columns, ",") != "size" {
		t.Errorf("the columns are %v", p.columns)
	}
	if len(p.rows) != 2 {
		t.Fatalf("%d rows: %v", len(p.rows), p.rows)
	}
	if strings.Join(p.rows[0], "|") != `96` {
		t.Errorf("the first row is %v", p.rows[0])
	}
	if strings.Join(p.rows[1], "|") != `310` {
		t.Errorf("the second row is %v", p.rows[1])
	}
}

// A file that says something a query file cannot say is refused rather than
// half-run.
func TestAFileThatIsNotAQueryIsRefused(t *testing.T) {
	src, err := source.ParsePSLSource(objects, source.Whole)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		`query 1 count=5`,
		`b=new button caption="press me"`,
	} {
		script, err := wire.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if err := run(src, script, &printer{}); err == nil {
			t.Errorf("%q was accepted", text)
		}
	}
}

// What `-raw` prints is the wire language the answer would have crossed as, so
// the word that says how much of each record came back is in it.
func TestTheRawTraceSaysHowMuchOfEachRecordCameBack(t *testing.T) {
	whole := rawDrive(t, `q=new query source="objects" sort={ .size desc } count=1`)
	if !strings.Contains(whole, `result 1 id=3 record={ key 3; .0 "src/parser.go"; .size 14022 }`) {
		t.Errorf("a record nothing narrowed was written as\n%s", whole)
	}

	// The same record, with the query naming the one field it wants: what goes
	// out is some of the record, and it says so.
	part := rawDrive(t,
		`q=new query source="objects" sort={ .size desc } count=1 fields={ .size }`)
	if !strings.Contains(part, `result 1 id=3 fields={ .size 14022 }`) {
		t.Errorf("a narrowed record was written as\n%s", part)
	}
}

// rawDrive runs a query file with the raw trace on and gives back what it
// printed.
func rawDrive(t *testing.T, query string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = saved; w.Close() }()
		src, err := source.ParsePSLSource(objects, source.Whole)
		if err != nil {
			t.Fatal(err)
		}
		script, err := wire.Parse(query)
		if err != nil {
			t.Fatal(err)
		}
		out := &printer{raw: true}
		if err := run(src, script, out); err != nil {
			t.Fatal(err)
		}
	}()
	text, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
