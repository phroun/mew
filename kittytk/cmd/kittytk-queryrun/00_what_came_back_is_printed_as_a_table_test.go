package main

// What the statements that came back look like once they are a table.
//
// The wire is covered where it belongs -- display/ drives a real application
// through a real display -- so what is left here is the reading: which
// statement ends the answer, what the terminator says happened, and a table
// that widens for a field an earlier record had not got.

import (
	"strings"
	"testing"
)

// feed hands the printer a run of statements and reports whether it stopped.
func feed(p *printer, lines ...string) bool {
	for _, line := range lines {
		if p.take(line) {
			return true
		}
	}
	return false
}

func TestWhatCameBackIsPrintedAsATable(t *testing.T) {
	p := &printer{}
	if !feed(p,
		`result 1 fields={ key 2; name "build.sh"; size 310 }`,
		`result 1 fields={ key 1; name "README.md"; size 2048 }`,
		`result 1 complete ordered watermark={ name "README.md"; key 1 }`,
	) {
		t.Fatal("the terminator did not end the answer")
	}
	if strings.Join(p.columns, ",") != "key,name,size" {
		t.Errorf("the columns are %v", p.columns)
	}
	if len(p.rows) != 2 {
		t.Fatalf("%d rows", len(p.rows))
	}
	if strings.Join(p.rows[0], "|") != `2|"build.sh"|310` {
		t.Errorf("the first row is %v", p.rows[0])
	}
	if !strings.Contains(p.note, "README.md") {
		t.Errorf("the watermark did not survive: %q", p.note)
	}
}

// Records need not carry the same fields: a window may ask for fewer than the
// query does, and a field a record has not got is not an error.
func TestATableWidensForAFieldAnEarlierRecordHadNot(t *testing.T) {
	p := &printer{}
	feed(p,
		`result 1 fields={ key 1; name "a" }`,
		`result 1 fields={ key 2; name "b"; size 99 }`,
		"result 1 complete exhausted",
	)
	if strings.Join(p.columns, ",") != "key,name,size" {
		t.Fatalf("the columns are %v", p.columns)
	}
	if p.rows[0][2] != "" {
		t.Errorf("the first row filled in a field it had not got: %v", p.rows[0])
	}
	if p.rows[1][2] != "99" {
		t.Errorf("the second row is %v", p.rows[1])
	}
	if !strings.Contains(p.note, "every record there is") {
		t.Errorf("exhausted did not survive: %q", p.note)
	}
}

// A refusal is an answer, and it ends the answer the same way.
func TestARefusalEndsTheAnswer(t *testing.T) {
	p := &printer{}
	if !feed(p, `result 1 complete error="no records past \"build.sh\""`) {
		t.Fatal("a refusal did not end the answer")
	}
	if !strings.Contains(p.note, "build.sh") {
		t.Errorf("the refusal reads %q", p.note)
	}
	if len(p.rows) != 0 {
		t.Errorf("a refusal produced %d row(s)", len(p.rows))
	}
}

// A batch the display refused never reaches an application, and says so.
func TestADisplayRefusalEndsTheAnswer(t *testing.T) {
	p := &printer{}
	if !feed(p, `error text="relay: nothing is connected as \"nobody\""`) {
		t.Fatal("a refused batch did not end the answer")
	}
	if !strings.Contains(p.note, "nobody") {
		t.Errorf("the refusal reads %q", p.note)
	}
}

// Anything that is not a result is not the answer, and does not end it. The
// display carries what the application said, and an application may say things
// of its own while it is answering.
func TestSomethingElseDoesNotEndTheAnswer(t *testing.T) {
	p := &printer{}
	if feed(p, "reply q=1", `set 5 caption="hello"`, "event click trinket=3") {
		t.Error("something that was not the answer ended it")
	}
	if len(p.rows) != 0 {
		t.Errorf("it produced %d row(s)", len(p.rows))
	}
}
