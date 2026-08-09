package keys

import (
	"strings"
	"testing"
)

// aliasHarness runs presses through a fresh processor over mappings and
// reports every command that fired (returned or dispatched through the
// executor) plus the final active sequence.
func aliasHarness(t *testing.T, mappings map[string]string, presses ...string) (fired []string, active string) {
	t.Helper()
	sp := NewSequenceProcessor(func(_, cmd string) bool { fired = append(fired, cmd); return true })
	sp.SetMappings(mappings)
	for _, k := range presses {
		if r := sp.ProcessKey(k); r.Command != "" {
			fired = append(fired, r.Command)
		}
	}
	return fired, sp.GetActiveSequence()
}

// A continuation key reaches the mapping whichever spelling the map uses:
// control, uppercase and lowercase forms of a letter are equivalent within a
// control-started sequence — the mid-progress sequence completes rather than
// being abandoned for a new one.
func TestSequenceContinuationAliases(t *testing.T) {
	base := map[string]string{
		"^C":   "cancel_binding",
		"^B P": "window_prior",
	}
	cases := []struct {
		name    string
		mapped  string // the ^B copy binding's spelling in the map
		presses []string
	}{
		{"upper mapping, control press", "^B C", []string{"^B", "^C"}},
		{"lower mapping, control press", "^B c", []string{"^B", "^C"}},
		{"lower mapping, upper press", "^B c", []string{"^B", "C"}},
		{"control mapping, upper press", "^B ^C", []string{"^B", "C"}},
		{"control mapping, lower press", "^B ^C", []string{"^B", "c"}},
	}
	for _, tc := range cases {
		m := map[string]string{tc.mapped: "buffer_duplicate"}
		for k, v := range base {
			m[k] = v
		}
		fired, active := aliasHarness(t, m, tc.presses...)
		if len(fired) != 1 || fired[0] != "buffer_duplicate" {
			t.Errorf("%s: fired %v, want [buffer_duplicate]", tc.name, fired)
		}
		if active != "" {
			t.Errorf("%s: sequence left active: %q", tc.name, active)
		}
	}
}

// The reported regression, end to end: with "^B c" mapped and ^C carrying its
// own binding AND starting sequences of its own, "^B ^C" must complete the ^B
// sequence — not fire ^C's binding, and not leave a "^C" sequence pending.
func TestSequenceAliasBeatsNewSequence(t *testing.T) {
	m := map[string]string{
		"^B c":    "buffer_duplicate",
		"^C":      "nav_cancel|cancel|viewport_close",
		"^C X":    "some_c_sequence",
		"^B P":    "window_prior",
		"^B help": `"keys_buffer"`,
	}
	fired, active := aliasHarness(t, m, "^B", "^C")
	if len(fired) != 1 || fired[0] != "buffer_duplicate" {
		t.Fatalf("mid-progress alias must beat a new sequence; fired %v", fired)
	}
	if active != "" {
		t.Fatalf("no ^C sequence should be picked up; active %q", active)
	}
}

// The as-pressed spelling always outranks an alias when both are mapped.
func TestSequenceAliasPriority(t *testing.T) {
	m := map[string]string{
		"^B ^C": "exact_control",
		"^B C":  "plain_upper",
	}
	fired, _ := aliasHarness(t, m, "^B", "^C")
	if len(fired) != 1 || fired[0] != "exact_control" {
		t.Fatalf("as-pressed must win over alias; fired %v", fired)
	}
	fired, _ = aliasHarness(t, m, "^B", "C")
	if len(fired) != 1 || fired[0] != "plain_upper" {
		t.Fatalf("as-pressed must win over alias; fired %v", fired)
	}
}

// Aliases combine across parts: a three-part chord matches with SEVERAL parts
// spelled differently from the map at once.
func TestSequenceAliasCrossProduct(t *testing.T) {
	m := map[string]string{
		"^O V ^C": "ruler_cursor",
		"^O A":    "other",
	}
	for _, presses := range [][]string{
		{"^O", "^V", "^C"}, // part 2 aliased
		{"^O", "V", "C"},   // part 3 aliased
		{"^O", "^V", "c"},  // parts 2 AND 3 aliased
	} {
		fired, active := aliasHarness(t, m, presses...)
		if len(fired) != 1 || fired[0] != "ruler_cursor" {
			t.Errorf("%v: fired %v, want [ruler_cursor]", presses, fired)
		}
		if active != "" {
			t.Errorf("%v: sequence left active: %q", presses, active)
		}
	}
}

// An aliased continuation that is a PREFIX of a longer mapping keeps the
// sequence alive: "^B ^C" stays pending when "^B C X" exists, and the final
// key completes it.
func TestSequenceAliasPrefixContinues(t *testing.T) {
	m := map[string]string{
		"^B C X": "deep_binding",
		"^C":     "cancel_binding",
	}
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(m)
	sp.ProcessKey("^B")
	sp.ProcessKey("^C")
	if got := sp.GetActiveSequence(); got != "^B ^C" {
		t.Fatalf("aliased prefix should stay pending; active %q", got)
	}
	r := sp.ProcessKey("X")
	if r.Command != "deep_binding" {
		t.Fatalf("completing the aliased prefix should fire deep_binding; got %q", r.Command)
	}
}

// The control-equivalence layer stays gated to control-started sequences: a
// plain-key starter does not equate ^C with c.
func TestSequenceAliasControlGate(t *testing.T) {
	m := map[string]string{
		"q c": "quick_thing",
	}
	fired, _ := aliasHarness(t, m, "q", "^C")
	for _, f := range fired {
		if f == "quick_thing" {
			t.Fatal("a non-control starter must not alias ^C to c")
		}
	}
}

// Named keys with control aliases resolve in both cases through the variant
// layer (return ~ ^M ~ M ~ m within a control-started sequence).
func TestSequenceAliasNamedControl(t *testing.T) {
	m := map[string]string{
		"^K m": "mark_binding",
	}
	fired, _ := aliasHarness(t, m, "^K", "return")
	if len(fired) != 1 || fired[0] != "mark_binding" {
		t.Fatalf("return should reach ^K m via ^M -> m; fired %v", fired)
	}
	if strings.Contains(strings.Join(fired, " "), "insert") {
		t.Fatalf("no fallthrough insert expected; fired %v", fired)
	}
}
