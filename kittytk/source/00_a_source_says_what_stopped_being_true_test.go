package source

// What an ApplicationSource does with a notice.
//
// The display-level test watches the screen follow one. This watches the seam
// itself, where the two halves of the rule can both be checked: a notice reaches
// the source it names and no other, and **nothing happens without one** -- no
// poll, no expiry, no generation compared.

import (
	"sync"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// heard is what one source was told, and how often.
type heard struct {
	mu     sync.Mutex
	stales []*wire.Stale
	tells  int
}

func (h *heard) count() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.stales), h.tells
}

func (h *heard) last() *wire.Stale {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.stales) == 0 {
		return nil
	}
	return h.stales[len(h.stales)-1]
}

// listening is a source with both tellings wired up, and nothing else.
func listening(t *testing.T, name string) (*ApplicationSource, *heard) {
	t.Helper()
	src := NewApplicationSource(name, func(string) error { return nil })
	h := &heard{}
	src.OnStale(func(n *wire.Stale) {
		h.mu.Lock()
		h.stales = append(h.stales, n)
		h.mu.Unlock()
	})
	src.WhenArrived(func() {
		h.mu.Lock()
		h.tells++
		h.mu.Unlock()
	})
	return src, h
}

// says hands one statement of wire text to the source, the way the connection's
// reader does, and reports whether it was taken.
func says(t *testing.T, src *ApplicationSource, text string) bool {
	t.Helper()
	script, err := wire.Parse(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("%s is %d statements", text, len(script.Statements))
	}
	return src.Inbound(script.Statements[0])
}

// **Nothing happens without a notice.** The whole arrangement stands on this:
// there is no poll here, nothing expires, and no generation is compared, so a
// source that is never told never tells anyone else.
func TestNothingIsSaidWithoutANotice(t *testing.T) {
	src, h := listening(t, "papers")
	if stales, tells := h.count(); stales != 0 || tells != 0 {
		t.Fatalf("a source that heard nothing reported %d notices and %d arrivals", stales, tells)
	}
	// Statements that are not this source's, handed over the way anything else on
	// the connection is. None of them is a notice and none of them is taken.
	for _, text := range []string{
		`event click trinket=7`,
		`init app=1 store=2`,
		`welcome version=1 session=1`,
	} {
		if says(t, src, text) {
			t.Errorf("%q was taken for this source's", text)
		}
	}
	if stales, tells := h.count(); stales != 0 || tells != 0 {
		t.Errorf("it reported %d notices and %d arrivals for statements it did not take", stales, tells)
	}
}

// A notice naming this source is taken, handed over whole, and the readers are
// told -- which is what makes the screen follow.
func TestANoticeForThisSourceIsTakenAndPassedOn(t *testing.T) {
	src, h := listening(t, "papers")
	if !says(t, src, `stale source="papers" id=42 how=altered fields={ size }`) {
		t.Fatal("a notice for this source was not taken")
	}
	stales, tells := h.count()
	if stales != 1 || tells != 1 {
		t.Fatalf("it reported %d notices and %d arrivals, want one of each", stales, tells)
	}
	// Handed over whole rather than acted on here, because what it COSTS depends
	// on what the holder keeps.
	got := h.last()
	if got.Source != "papers" {
		t.Errorf("it is about %q", got.Source)
	}
	if got.ID == nil || got.ID.Int != 42 {
		t.Errorf("it names %v", got.ID)
	}
	if got.How.String() != "altered" || len(got.Fields) != 1 || got.Fields[0] != "size" {
		t.Errorf("it reads as %s %v", got.How, got.Fields)
	}
}

// **A notice for another name is not this one's**, which is what lets one
// connection serve several sources. It is not taken, so the connection goes on
// offering it to whoever else is listening.
func TestANoticeForAnotherSourceIsLeftAlone(t *testing.T) {
	papers, ph := listening(t, "papers")
	ledgers, lh := listening(t, "ledgers")

	text := `stale source="ledgers" how=replaced`
	if says(t, papers, text) {
		t.Error("papers took a notice about ledgers")
	}
	if stales, tells := ph.count(); stales != 0 || tells != 0 {
		t.Errorf("papers reported %d notices and %d arrivals", stales, tells)
	}
	if !says(t, ledgers, text) {
		t.Error("ledgers did not take its own notice")
	}
	if stales, _ := lh.count(); stales != 1 {
		t.Errorf("ledgers reported %d notices", stales)
	}
}

// A notice that does not parse is not taken, and nothing half-applies it. The
// statement goes on to whoever else may want it, and nobody is told anything.
func TestANoticeThatDoesNotParseIsNotTaken(t *testing.T) {
	src, h := listening(t, "papers")
	for _, text := range []string{
		// `fields=` beside a removal contradicts the word next to it.
		`stale source="papers" id=42 how=removed fields={ size }`,
		// Not one of the four.
		`stale source="papers" id=42 how=changed`,
		// It names fields, never their values.
		`stale source="papers" id=42 how=altered fields={ size 1024 }`,
		// An argument it does not know is refused rather than ignored.
		`stale source="papers" id=42 how=removed reason="deleted"`,
	} {
		if says(t, src, text) {
			t.Errorf("%s was taken", text)
		}
	}
	if stales, tells := h.count(); stales != 0 || tells != 0 {
		t.Errorf("a notice that does not parse reported %d notices and %d arrivals", stales, tells)
	}
}

// The widest notice is still a notice: no record named, and the readers are told
// just the same. A source that cannot tell what moved says this rather than
// saying nothing, and coarsening is always safe.
func TestTheWidestNoticeStillTells(t *testing.T) {
	src, h := listening(t, "papers")
	if !says(t, src, `stale source="papers"`) {
		t.Fatal("the widest notice was not taken")
	}
	if got := h.last(); got == nil || got.ID != nil {
		t.Errorf("it named a record: %v", got)
	}
	if _, tells := h.count(); tells != 1 {
		t.Errorf("it told the readers %d times", tells)
	}
}
