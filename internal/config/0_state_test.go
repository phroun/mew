package config

// Persisted state in both formats, and what a document can hold.
//
// PSL and JSON have to mean the same thing here, because a host may write one
// and read the other. JSON has no way to say a list that is ordered and named
// at once, so state does not use one, and a document that does is refused by
// name rather than half-read.

import (
	"strings"
	"testing"
)

// What goes out comes back, with nested containers as plain Go shapes whichever
// format carried them.
func TestStateComesBackAsPlainMapsAndSlices(t *testing.T) {
	state := map[string]interface{}{
		"theme":  "dark",
		"window": map[string]interface{}{"width": int64(120), "height": int64(40)},
		"recent": []interface{}{"a.go", "b.md"},
	}

	for _, format := range []StateFormat{FormatPSL, FormatJSON} {
		out, err := EncodeState(state, format)
		if err != nil {
			t.Fatalf("EncodeState(%v): %v", format, err)
		}
		back, err := DecodeState(out)
		if err != nil {
			t.Fatalf("DecodeState(%v): %v\n%s", format, err, out)
		}

		if back["theme"] != "dark" {
			t.Errorf("%v: theme came back as %#v", format, back["theme"])
		}
		window, ok := back["window"].(map[string]interface{})
		if !ok {
			t.Fatalf("%v: window came back as %T: %s", format, back["window"], out)
		}
		if got := window["width"]; !sameNumber(got, 120) {
			t.Errorf("%v: window width came back as %#v", format, got)
		}
		recent, ok := back["recent"].([]interface{})
		if !ok {
			t.Fatalf("%v: recent came back as %T: %s", format, back["recent"], out)
		}
		if len(recent) != 2 || recent[0] != "a.go" {
			t.Errorf("%v: recent came back as %#v", format, recent)
		}
	}
}

// A list that is ordered and named at once cannot be said in JSON, so it is
// refused, and the message says which value it was.
func TestAListThatIsOrderedAndNamedAtOnceIsRefusedByName(t *testing.T) {
	_, err := DecodeState(`(theme: "dark", window: (width: 120, "stray"))`)
	if err == nil {
		t.Fatal("a list holding both was accepted")
	}
	if !strings.Contains(err.Error(), "window") {
		t.Errorf("the error does not name the value: %v", err)
	}
}

// The same, one level further in, named by its path.
func TestAListThatIsOrderedAndNamedAtOnceIsFoundAtDepth(t *testing.T) {
	_, err := DecodeState(`(window: (panes: ("left", (a: 1, "b"))))`)
	if err == nil {
		t.Fatal("a list holding both was accepted")
	}
	if !strings.Contains(err.Error(), "window.panes[1]") {
		t.Errorf("the error does not say where the value is: %v", err)
	}
}

// State is a set of named values, so a document with values that have no name
// is refused rather than filed under invented keys.
func TestAStateValueWithoutANameIsRefused(t *testing.T) {
	if _, err := DecodeState(`("first", "second")`); err == nil {
		t.Error("unnamed top-level values were accepted")
	}
	if _, err := DecodeState(`(theme: "dark", "stray")`); err == nil {
		t.Error("a stray unnamed value was accepted")
	}
}

// Empty input is empty state, not an error, and a document in neither format is
// refused.
func TestEmptyAndUnrecognizedState(t *testing.T) {
	for _, in := range []string{"", "   ", "\n"} {
		got, err := DecodeState(in)
		if err != nil || len(got) != 0 {
			t.Errorf("DecodeState(%q) = %#v, %v", in, got, err)
		}
	}
	if _, err := DecodeState(`theme: "dark"`); err == nil {
		t.Error("a document in neither format was accepted")
	}
}

// A list with nothing in it is a list, and a document with nothing in it is
// still a document.
func TestAnEmptyListAndAnEmptyDocument(t *testing.T) {
	back, err := DecodeState(`(recent: (), window: ())`)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	for _, key := range []string{"recent", "window"} {
		if _, ok := back[key].(map[string]interface{}); !ok {
			t.Errorf("%s came back as %T, want an empty map", key, back[key])
		}
	}
	if got, err := DecodeState("()"); err != nil || len(got) != 0 {
		t.Errorf("DecodeState(\"()\") = %#v, %v", got, err)
	}
}

// sameNumber compares a decoded number against a want, whichever native type
// its format decodes to: int64 from PSL, float64 from JSON.
func sameNumber(got interface{}, want float64) bool {
	switch n := got.(type) {
	case int64:
		return float64(n) == want
	case float64:
		return n == want
	}
	return false
}
