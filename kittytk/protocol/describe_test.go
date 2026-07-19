package protocol

import (
	"strings"
	"testing"
)

// The fluent builders set kind/default/doc/enum on a Property's descriptor.
func TestPropertyBuilders(t *testing.T) {
	p := NewProperty("string", nil).Tip("hello").Def("x")
	if p.Desc.Kind != "string" || p.Desc.Doc != "hello" || p.Desc.Default != "x" {
		t.Errorf("builder: %+v", p.Desc)
	}
	e := NewProperty("word", nil).OneOf("a", "b")
	if e.Desc.Kind != "enum" || len(e.Desc.Enum) != 2 {
		t.Errorf("OneOf should mark enum: %+v", e.Desc)
	}
}

// A described vocabulary encodes to FLAT statements (one per line, no
// nested blocks) and decodes back to the same structure.
func TestVocabularyEncodeDecodeRoundTrip(t *testing.T) {
	v := &Vocabulary{
		Common: []PropInfo{
			{Name: "enabled", Kind: "flag", Default: "true", Doc: "On/off."},
		},
		Types: []TypeInfo{
			{Name: "button", Props: []PropInfo{
				{Name: "caption", Kind: "string", Doc: "Label."},
				{Name: "align", Kind: "enum", Doc: "Alignment.", Enum: []string{"left", "right"}},
			}},
			{Name: "item", Virtual: true, Props: []PropInfo{
				{Name: "caption", Kind: "string", Doc: "Row text."},
			}},
		},
	}

	enc := EncodeVocabulary(v)
	// Flat: every line is a single statement with no unescaped braces.
	for _, line := range strings.Split(strings.TrimRight(enc, "\n"), "\n") {
		if strings.ContainsAny(line, "{}") {
			t.Errorf("describe output must be flat, got brace in: %q", line)
		}
	}

	lines := strings.Split(strings.TrimRight(enc, "\n"), "\n")
	got, err := DecodeVocabulary(lines)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Common) != 1 || got.Common[0].Name != "enabled" || got.Common[0].Default != "true" {
		t.Errorf("common round-trip: %+v", got.Common)
	}
	if len(got.Types) != 2 {
		t.Fatalf("types: got %d, want 2", len(got.Types))
	}
	btn := got.Types[0]
	if btn.Name != "button" || btn.Virtual {
		t.Errorf("button type: %+v", btn)
	}
	if len(btn.Props) != 2 || btn.Props[1].Name != "align" || len(btn.Props[1].Enum) != 2 {
		t.Errorf("button props round-trip: %+v", btn.Props)
	}
	if !got.Types[1].Virtual {
		t.Errorf("item should decode as virtual: %+v", got.Types[1])
	}
}
