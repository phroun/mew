package keys

import (
	"reflect"
	"testing"
)

// captureHarness is a scripted executor: it records every (key, command) the
// processor dispatches and answers with the scripted status — commands absent
// from the script are handled (true). Declining (false) is how a binding
// says "not mine", so the script is how a test plays a terminal that is or
// is not there to take a key.
type captureHarness struct {
	calls   []string        // "key→command", in dispatch order
	decline map[string]bool // command -> decline it (report unhandled)
}

func (h *captureHarness) exec(key, command string) bool {
	h.calls = append(h.calls, key+"→"+command)
	return !h.decline[command]
}

func newCaptureSP(mappings map[string]string, decline ...string) (*SequenceProcessor, *captureHarness) {
	h := &captureHarness{decline: map[string]bool{}}
	for _, d := range decline {
		h.decline[d] = true
	}
	sp := NewSequenceProcessor(h.exec)
	sp.SetMappings(mappings)
	return sp, h
}

// Level dominates specificity at equal length: the whole point of the capture
// layer is that a hosted terminal's wildcard takes keys mew's base set has
// bound. Up is bound at level 0, captured at level 1 — the terminal wins.
func TestCaptureLevelOutranksABoundKey(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"up":        "go_line_prior",
		"capture *": "tinput_key",
	})
	sp.ProcessKey("up")
	if want := []string{"up→tinput_key"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — the capture level must win over the bound key", h.calls, want)
	}
}

// A candidate declining drops resolution one level: no terminal under the
// wildcard, and the key lands exactly where it always did.
func TestDecliningDescendsToTheLevelBelow(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"up":        "go_line_prior",
		"capture *": "tinput_key",
	}, "tinput_key")
	sp.ProcessKey("up")
	want := []string{"up→tinput_key", "up→go_line_prior"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Within a level, a named key SHADOWS the wildcard — the wildcard is a
// level's answer only for keys the level does not name.
func TestSpecificShadowsTheWildcardWithinALevel(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"capture del": `tinput "\x08"`,
		"capture *":   "tinput_key",
	})
	sp.ProcessKey("del")
	if want := []string{`del→tinput "\x08"`}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// The reclaim idiom: a capture bound to false declines its key PAST the whole
// capture level — including that level's own wildcard, which must not catch
// the key on the way down — so it lands at the layers below.
func TestReclaimSkipsTheCapturingLevelEntirely(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"capture ^C": "false",
		"capture *":  "tinput_key",
		"^C":         "viewport_close",
	}, "false")
	sp.ProcessKey("^C")
	want := []string{"^C→false", "^C→viewport_close"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — the reclaim must not fall into its own level's wildcard", h.calls, want)
	}
}

// The counterpart: a capture bound to true is a successful no-op that
// squelches every layer below.
func TestCaptureTrueSquelchesTheLevelsBelow(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"capture ^C": "true",
		"^C":         "viewport_close",
	})
	sp.ProcessKey("^C")
	if want := []string{"^C→true"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Rule 1: a longer sequence possibility outranks every single-key claim,
// wildcard included. ^B is held — nothing dispatches — and N completes the
// sequence at level 0.
func TestSequenceInProgressOutranksTheWildcard(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B N":      "buffer_next",
		"capture *": "tinput_key",
	})
	sp.ProcessKey("^B")
	if len(h.calls) != 0 {
		t.Fatalf("^B should be HELD as a possible prefix, but dispatched %v", h.calls)
	}
	if sp.GetActiveSequence() != "^B" {
		t.Fatalf("active sequence %q, want ^B", sp.GetActiveSequence())
	}
	sp.ProcessKey("N")
	if want := []string{"N→buffer_next"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// A disqualified sequence unwinds: the held starter is RELEASED to the
// wildcard (this is how ^B reaches a shell), and the invalid key replays as
// an ordinary key — which the wildcard also takes.
func TestUnwindReleasesTheStarterToTheCapture(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B N":      "buffer_next",
		"capture *": "tinput_key",
	})
	sp.ProcessKey("^B")
	sp.ProcessKey("q")
	want := []string{"^B→tinput_key", "q→tinput_key"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Without a capture in the map, the unwind keeps its pre-capture shape: a
// control starter is dropped, and the invalid key takes its ordinary path.
func TestUnwindWithoutACaptureKeepsTheOldShape(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B N": "buffer_next",
	})
	sp.ProcessKey("^B")
	sp.ProcessKey("q")
	if want := []string{"q→insert 'q'"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — ^B dropped, q inserted, as before", h.calls, want)
	}
}

// A matched sequence is CONSUMED even when every level declines it: ^B N
// failing must not unwind into the buffer or a child as the letter N.
func TestADeclinedSequenceDoesNotUnwind(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B N": "buffer_next",
	}, "buffer_next")
	sp.ProcessKey("^B")
	sp.ProcessKey("N")
	if want := []string{"N→buffer_next"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — nothing after the decline", h.calls, want)
	}
}

// When every level declines a single key, the default-handling floor runs —
// what an unbound key would have done all along. This is load-bearing for
// reclaim: a declined ^C must land on its classic cancel|viewport_close.
func TestTheFloorRunsWhenEveryLevelDeclines(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"capture *": "tinput_key",
	}, "tinput_key")
	sp.ProcessKey("x")
	want := []string{"x→tinput_key", "x→insert 'x'"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Compounding: each "capture" adds one level and "override" adds two, so a
// user can always outbid a layer by writing one more word.
func TestCaptureWordsCompound(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"capture ^C":         "level_one",
		"capture capture ^C": "level_two",
	})
	sp.ProcessKey("^C")
	if want := []string{"^C→level_two"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}

	sp2, h2 := newCaptureSP(map[string]string{
		"capture capture ^C":  "level_two",
		"override ^C":         "also_level_two_but_spelled_override",
		"override capture ^C": "level_three",
	})
	sp2.ProcessKey("^C")
	if want := []string{"^C→level_three"}; !reflect.DeepEqual(h2.calls, want) {
		t.Errorf("dispatched %v, want %v", h2.calls, want)
	}
}

// The pending short-match machinery (a bound prefix of a longer binding)
// still fires and replays across the new resolution.
func TestPendingShortMatchStillFires(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^K X":   "short_cmd",
		"^K X Y": "long_cmd",
	})
	sp.ProcessKey("^K")
	sp.ProcessKey("X")
	if len(h.calls) != 0 {
		t.Fatalf("^K X should be pending (a longer match exists), dispatched %v", h.calls)
	}
	sp.ProcessKey("q")
	want := []string{"X→short_cmd", "q→insert 'q'"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Resolution-only mode (nil executor): there is no status to descend on, so
// ProcessKey reports the top-precedence command — the old single-command
// contract, which the rest of the test suite still leans on.
func TestResolutionOnlyModeReportsTheTopCandidate(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{
		"up":        "go_line_prior",
		"capture *": "tinput_key",
	})
	if got := sp.ProcessKey("up").Command; got != "tinput_key" {
		t.Errorf("Command = %q, want the top candidate tinput_key", got)
	}
	if got := sp.ProcessKey("z").Command; got != "tinput_key" {
		t.Errorf("Command = %q, want tinput_key for an unbound key too", got)
	}
}

// DisplayKey strips the precedence words — a user presses the same keys at
// every level — and disowns the wildcard, which names no pressable key.
func TestDisplayKey(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{"^K B", "^K B", true},
		{"capture ^C", "^C", true},
		{"override capture up", "up", true},
		{"capture *", "", false},
		{"*", "", false},
		{"capture", "capture", true}, // the literal word as a key: level 0
	} {
		got, ok := DisplayKey(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("DisplayKey(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

// GetMapping and GetAllMappings speak raw spellings, so config merging and
// provenance see exactly what the user wrote.
func TestRawSpellingsSurviveTheRoundTrip(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{"capture *": "tinput_key"})
	if got := sp.GetMapping("capture *"); got != "tinput_key" {
		t.Errorf("GetMapping = %q, want tinput_key", got)
	}
	all := sp.GetAllMappings()
	if all["capture *"] != "tinput_key" {
		t.Errorf("GetAllMappings = %v, want the raw capture spelling preserved", all)
	}
	sp.UnmapKey("capture *")
	if got := sp.ProcessKey("z").Command; got != "insert 'z'" {
		t.Errorf("after unmap, Command = %q, want the default insert", got)
	}
}

// A NAMED single-key capture claims the key outright: lower levels' sequences
// starting with it stop being live, so the key resolves as a single key
// instead of being held as a prefix. Naming Escape at a terminal's level means
// Escape belongs to the child, not that it does unless mew happens to have an
// esc-chord.
func TestNamedCaptureSuppressesLowerLevelSequences(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"esc X":       "cmd",
		"esc y":       "kill_ring_yank",
		"capture esc": "tinput_key",
	})
	sp.ProcessKey("esc")
	if got := sp.GetActiveSequence(); got != "" {
		t.Fatalf("esc was held as a prefix (%q); a named capture must claim it outright", got)
	}
	if want := []string{"esc→tinput_key"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// The WILDCARD does not suppress. It is the "everything else" net, and if it
// suppressed, `capture * = tinput_key` would silently kill ^B N and every
// other chord the moment a terminal took focus. This is the asymmetry that
// makes named capture safe to have.
func TestWildcardDoesNotSuppressSequences(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B N":      "buffer_next",
		"capture *": "tinput_key",
	})
	sp.ProcessKey("^B")
	if sp.GetActiveSequence() != "^B" {
		t.Fatalf("the wildcard suppressed a chord: activeSeq=%q, want ^B held", sp.GetActiveSequence())
	}
	sp.ProcessKey("N")
	if want := []string{"N→buffer_next"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}

// Suppression reaches only levels BELOW the claim: a level's own chords are
// its business, and a chord ABOVE the claim outranks it.
func TestSuppressionOnlyReachesLowerLevels(t *testing.T) {
	// Same level: the chord survives, and rule 1 still holds the prefix.
	sp, _ := newCaptureSP(map[string]string{
		"capture esc":   "tinput_key",
		"capture esc X": "still_mine",
	})
	sp.ProcessKey("esc")
	if sp.GetActiveSequence() != "esc" {
		t.Errorf("a level must not suppress its own chord: activeSeq=%q", sp.GetActiveSequence())
	}

	// Higher level: the chord outranks the claim below it.
	sp2, _ := newCaptureSP(map[string]string{
		"capture esc":           "tinput_key",
		"capture capture esc X": "outranks",
	})
	sp2.ProcessKey("esc")
	if sp2.GetActiveSequence() != "esc" {
		t.Errorf("a chord above the claim must survive: activeSeq=%q", sp2.GetActiveSequence())
	}
}

// With no capture anywhere, nothing changes: esc is a prefix exactly as it
// always was.
func TestEscStaysAPrefixWithoutACapture(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"esc X": "cmd",
		"esc y": "kill_ring_yank",
	})
	sp.ProcessKey("esc")
	if sp.GetActiveSequence() != "esc" {
		t.Fatalf("activeSeq=%q, want esc held", sp.GetActiveSequence())
	}
	sp.ProcessKey("X")
	if want := []string{"X→cmd"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v", h.calls, want)
	}
}
