package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

func newFourWindowMDI(t *testing.T) *MDIPane {
	t.Helper()
	m := NewMDIPane()
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})
	for _, name := range []string{"A", "B", "C", "D"} {
		w := window.NewWindow(name)
		w.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
		m.AddWindow(w) // AddWindow activates the newcomer
	}
	// After the adds, D is active and the sequence is [A,B,C,D].
	if got := m.ActiveWindow().Title(); got != "D" {
		t.Fatalf("setup: active = %q, want D", got)
	}
	return m
}

func mdiCycleSeq(m *MDIPane, forward bool, n int) []string {
	seq := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if forward {
			m.NextWindow()
		} else {
			m.PrevWindow()
		}
		seq = append(seq, m.ActiveWindow().Title())
	}
	return seq
}

// Forward MDI cycling (Next) steps toward the most recently used child: with D
// most recent, one press lands on C, then B, A, and wraps to D. It must walk
// the full set instead of ping-ponging (the defect bringToFront caused by
// reordering the list being iterated).
func TestMDIForwardCycleTraversesAll(t *testing.T) {
	m := newFourWindowMDI(t)
	got := mdiCycleSeq(m, true, 4)
	want := []string{"C", "B", "A", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("MDI forward cycle = %v, want %v", got, want)
	}
}

// Backward MDI cycling (Prev) heads the other way, reaching the least recently
// used first: A, B, C, then back to D.
func TestMDIBackwardCycleTraversesAll(t *testing.T) {
	m := newFourWindowMDI(t)
	got := mdiCycleSeq(m, false, 4)
	want := []string{"A", "B", "C", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("MDI backward cycle = %v, want %v", got, want)
	}
}

// Stepping (as the parent's Next/Prev buttons do) must not commit the sequence:
// endCycleSession is what promotes the landing spot. Cycling forward to C then
// committing makes a fresh forward step toggle back to D.
func TestMDICommitOnInteraction(t *testing.T) {
	m := newFourWindowMDI(t)

	m.NextWindow() // land on C, sequence frozen [A,B,C,D]
	if got := m.ActiveWindow().Title(); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}
	m.endCycleSession() // stands in for a child key/click interaction

	// Committed (C most recent, D second): a fresh Next toggles back to D.
	m.NextWindow()
	if got := m.ActiveWindow().Title(); got != "D" {
		t.Errorf("after commit, Next from C = %q, want D (toggle)", got)
	}
}

// The idle lock-in commits a run whose last step was long ago, then starts a
// fresh gesture - MDI's only commit-on-settle signal (no modifier to release).
func TestMDIIdleLockInCommitsPriorRun(t *testing.T) {
	m := newFourWindowMDI(t)

	m.NextWindow() // land on C
	if got := m.ActiveWindow().Title(); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}

	// Simulate more than the lock-in timeout since that step.
	m.mu.Lock()
	m.lastCycleAt = m.lastCycleAt.Add(-2 * mdiCycleCommitTimeout)
	m.mu.Unlock()

	// New gesture: locks C in first, then Next toggles back to D.
	m.NextWindow()
	if got := m.ActiveWindow().Title(); got != "D" {
		t.Errorf("after idle lock-in, Next from C = %q, want D", got)
	}
}
