package client_test

import (
	"testing"

	"github.com/phroun/kittytk/client"
)

// Describe (D24) returns the host's wire vocabulary: common properties
// plus each registered type with its properties' kind/default/doc.
func TestDescribeVocabulary(t *testing.T) {
	conn := client.NewInProcess(nil)
	defer conn.Close()

	vocab, err := conn.Describe()
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	// Common properties are reported once and carry descriptors.
	common := map[string]bool{}
	for _, p := range vocab.Common {
		common[p.Name] = true
		if p.Doc == "" {
			t.Errorf("common property %q has no description", p.Name)
		}
	}
	for _, want := range []string{"enabled", "visible", "name", "fg", "bg"} {
		if !common[want] {
			t.Errorf("common property %q missing from vocabulary", want)
		}
	}

	// The button type is present with a described caption property.
	types := map[string][]struct{ name, kind, doc string }{}
	for _, ty := range vocab.Types {
		for _, p := range ty.Props {
			types[ty.Name] = append(types[ty.Name], struct{ name, kind, doc string }{p.Name, p.Kind, p.Doc})
		}
	}
	btn, ok := types["button"]
	if !ok {
		t.Fatalf("button type missing from vocabulary (%d types)", len(vocab.Types))
	}
	foundCaption := false
	for _, p := range btn {
		if p.name == "caption" {
			foundCaption = true
			if p.kind != "string" {
				t.Errorf("button.caption kind = %q, want string", p.kind)
			}
			if p.doc == "" {
				t.Errorf("button.caption has no description")
			}
		}
	}
	if !foundCaption {
		t.Error("button.caption missing from vocabulary")
	}
}

// Every registered property (common and type-specific) carries a
// non-empty description and a kind, so the vocabulary is fully
// self-documenting.
func TestDescribeCoverage(t *testing.T) {
	conn := client.NewInProcess(nil)
	defer conn.Close()
	vocab, err := conn.Describe()
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	check := func(owner string, p struct {
		Name, Kind, Default, Doc string
		Enum                     []string
	}) {
		if p.Kind == "" {
			t.Errorf("%s.%s has no kind", owner, p.Name)
		}
		if p.Doc == "" {
			t.Errorf("%s.%s has no description", owner, p.Name)
		}
	}
	for _, p := range vocab.Common {
		check("(common)", struct {
			Name, Kind, Default, Doc string
			Enum                     []string
		}{p.Name, p.Kind, p.Default, p.Doc, p.Enum})
	}
	for _, ty := range vocab.Types {
		for _, p := range ty.Props {
			check(ty.Name, struct {
				Name, Kind, Default, Doc string
				Enum                     []string
			}{p.Name, p.Kind, p.Default, p.Doc, p.Enum})
		}
	}
}
