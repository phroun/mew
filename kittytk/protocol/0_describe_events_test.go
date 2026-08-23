package protocol

import (
	"strings"
	"testing"
)

// Field appends in declared order, and a chain that branches does not
// write over the branch it grew from.
//
// EventDesc is a value written into map literals, so two chains built
// from one base share that base's backing array unless Field copies. The
// symptom would be a field silently belonging to the wrong event.
func TestEventDescFieldDoesNotAliasABranch(t *testing.T) {
	base := NewEventDesc("base").Field("trinket", "uint", "The object ID.")
	a := base.Field("text", "string", "The content.")
	b := base.Field("selected", "int", "The index.")

	if len(base.Fields) != 1 {
		t.Errorf("base grew to %d fields; a chain wrote back into it", len(base.Fields))
	}
	if len(a.Fields) != 2 || a.Fields[1].Name != "text" {
		t.Errorf("branch a: %+v", a.Fields)
	}
	if len(b.Fields) != 2 || b.Fields[1].Name != "selected" {
		t.Errorf("branch b: %+v", b.Fields)
	}
}

// Events encode to flat statements alongside properties, and decode back
// to the same events with the same fields in the same order.
func TestVocabularyEventsRoundTrip(t *testing.T) {
	v := &Vocabulary{
		Types: []TypeInfo{
			{Name: "button", Props: []PropInfo{
				{Name: "caption", Kind: "string", Doc: "Label."},
			}, Events: []EventInfo{
				{Name: "click", Doc: "Activated.", Fields: []EventFieldDesc{
					{Name: "trinket", Kind: "uint", Doc: "The button."},
				}},
			}},
			{Name: "listview", Events: []EventInfo{
				{Name: "activate", Doc: "Row activated.", Fields: []EventFieldDesc{
					{Name: "trinket", Kind: "uint", Doc: "The list."},
					{Name: "selected", Kind: "int", Doc: "The row."},
				}},
				{Name: "change", Doc: "Selection moved.", Fields: []EventFieldDesc{
					{Name: "trinket", Kind: "uint", Doc: "The list."},
					{Name: "selected", Kind: "int", Doc: "The row."},
				}},
			}},
		},
	}

	enc := EncodeVocabulary(v)
	lines := strings.Split(strings.TrimRight(enc, "\n"), "\n")
	for _, line := range lines {
		if strings.ContainsAny(line, "{}") {
			t.Errorf("describe output must stay flat, got brace in: %q", line)
		}
	}

	got, err := DecodeVocabulary(lines)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Types) != 2 {
		t.Fatalf("types: got %d, want 2", len(got.Types))
	}

	btn := got.Types[0]
	if len(btn.Events) != 1 || btn.Events[0].Name != "click" || btn.Events[0].Doc != "Activated." {
		t.Fatalf("button events: %+v", btn.Events)
	}
	if len(btn.Events[0].Fields) != 1 || btn.Events[0].Fields[0].Kind != "uint" {
		t.Errorf("click fields: %+v", btn.Events[0].Fields)
	}

	// Two events on one type keep their own fields: the decoder matches an
	// eventfield to its event by name, so neither collects the other's.
	lv := got.Types[1]
	if len(lv.Events) != 2 {
		t.Fatalf("listview events: %+v", lv.Events)
	}
	for _, e := range lv.Events {
		if len(e.Fields) != 2 {
			t.Errorf("event %q got %d fields, want 2: %+v", e.Name, len(e.Fields), e.Fields)
		}
		if e.Fields[0].Name != "trinket" || e.Fields[1].Name != "selected" {
			t.Errorf("event %q field order: %+v", e.Name, e.Fields)
		}
	}
}

// An eventfield naming an event that was never declared is dropped
// rather than inventing one, and an unknown statement kind is ignored —
// which is what lets a newer host add statements an older client has
// never heard of.
func TestDecodeToleratesUnknownStatements(t *testing.T) {
	lines := []string{
		`proptype name="button" !virtual`,
		`event of="button" name="click" doc="Activated."`,
		`eventfield of="button" event="nosuch" name="x" kind="int" doc="Stray."`,
		`somethingnew of="button" name="later"`,
	}
	got, err := DecodeVocabulary(lines)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Types) != 1 || len(got.Types[0].Events) != 1 {
		t.Fatalf("types: %+v", got.Types)
	}
	if len(got.Types[0].Events[0].Fields) != 0 {
		t.Errorf("stray eventfield was attached: %+v", got.Types[0].Events[0].Fields)
	}
}

// A type that declares no events describes as having none, rather than
// as an empty list that would render an empty table.
func TestNoEventsDescribesAsNil(t *testing.T) {
	if got := sortedEventInfos(nil); got != nil {
		t.Errorf("nil events: got %+v, want nil", got)
	}
	if got := sortedEventInfos(map[string]EventDesc{}); got != nil {
		t.Errorf("empty events: got %+v, want nil", got)
	}
}
