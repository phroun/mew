package window

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// activeTitle returns the active window's title, or "" when the dock (nil) is
// the current selection.
func activeTitle(m *WindowManager) string {
	if w := m.ActiveWindow(); w != nil {
		return w.Title()
	}
	return ""
}

// cycleSeq runs n cycle steps in the given direction and records the active
// window title after each step.
func cycleSeq(m *WindowManager, forward bool, n int) []string {
	seq := make([]string, 0, n)
	for i := 0; i < n; i++ {
		m.CycleWindows(forward)
		seq = append(seq, activeTitle(m))
	}
	return seq
}

func newFourWindowManager(t *testing.T) (*WindowManager, [4]*Window) {
	t.Helper()
	m := NewWindowManager()
	var ws [4]*Window
	for i, name := range []string{"A", "B", "C", "D"} {
		ws[i] = NewWindow(name)
		m.AddWindow(ws[i])
	}
	m.ActivateWindow(ws[3]) // known start: cycleOrder [A,B,C,D], D most recent
	return m, ws
}

// Forward cycling (Alt-Tab) steps toward the most recently used window: with D
// most recent, one press lands on the next-most-recent C, then B, then A, then
// wraps back to D. It must walk the full set, not ping-pong.
func TestCycleWindowsForwardTraversesAll(t *testing.T) {
	m, _ := newFourWindowManager(t)

	got := cycleSeq(m, true, 4)
	want := []string{"C", "B", "A", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("forward cycle = %v, want %v", got, want)
	}
}

// Backward cycling (Shift-Alt-Tab) heads the other way, reaching the least
// recently used first: A, then B, C, and finally back to D. It must walk the
// full set, not ping-pong between the two most-recent windows.
func TestCycleWindowsBackwardTraversesAll(t *testing.T) {
	m, _ := newFourWindowManager(t)

	got := cycleSeq(m, false, 4)
	want := []string{"A", "B", "C", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("backward cycle = %v, want %v", got, want)
	}
}

// A window added mid-run is picked up because the run reads the live cycle
// list, not a frozen snapshot: continued cycling still reaches every window,
// the newcomer included, with no window orphaned and no ping-pong. (Adding a
// window activates it, which also commits the run - so the newcomer is where
// the next steps continue from.)
func TestCycleWindowsPicksUpWindowAddedMidRun(t *testing.T) {
	m, _ := newFourWindowManager(t)

	// Step forward once, then add E while the run is live.
	m.CycleWindows(true)
	e := NewWindow("E")
	m.AddWindow(e)

	// Over a full lap, every window (including the newcomer E) is reachable.
	seen := map[string]bool{activeTitle(m): true}
	for _, title := range cycleSeq(m, true, 5) {
		seen[title] = true
	}
	for _, name := range []string{"A", "B", "C", "D", "E"} {
		if !seen[name] {
			t.Errorf("window %q was not reachable after a mid-run add; seen=%v", name, seen)
		}
	}
}

// A closed window drops out of the run: the live list no longer contains it.
func TestCycleWindowsSkipsWindowRemovedMidRun(t *testing.T) {
	m, ws := newFourWindowManager(t)

	// Step forward once: lands on C. Order frozen [A,B,C,D].
	m.CycleWindows(true)
	if got := activeTitle(m); got != "C" {
		t.Fatalf("first step = %q, want C", got)
	}

	// Remove A (not active). Live order becomes [B,C,D].
	m.RemoveWindow(ws[0])

	got := cycleSeq(m, true, 3)
	want := []string{"B", "D", "C"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cycle after mid-run remove = %v, want %v", got, want)
	}
	for _, title := range got {
		if title == "A" {
			t.Error("removed window A must not appear in the cycle")
		}
	}
}

// A genuine window interaction ends a run and commits the landing spot to the
// MRU front, so the next independent run starts from there - this is what makes
// Alt-Tab toggle between the two most-recent windows. (endCycleSession is the
// commit primitive; in the desktop it fires when the active window itself
// handles a key, or on a window click/activation - not on menu-bar keys.)
func TestCycleWindowsCommitsMRUOnSessionEnd(t *testing.T) {
	m, _ := newFourWindowManager(t)

	// Alt-Tab once to C (the most-recently-used other window), then a genuine
	// interaction commits it.
	m.CycleWindows(true)
	if got := activeTitle(m); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}
	m.endCycleSession() // stands in for a window-handled key / click

	// C is now most-recent, D second. A fresh Alt-Tab toggles back to D.
	m.CycleWindows(true)
	if got := activeTitle(m); got != "D" {
		t.Errorf("after commit, Alt-Tab from C = %q, want D (toggle)", got)
	}
}

// On surfaces that deliver key releases (SDL), a run commits the moment all
// modifiers rise: NotifyModifiersReleased promotes the landing spot.
func TestCycleWindowsCommitsOnModifiersReleased(t *testing.T) {
	m, _ := newFourWindowManager(t)
	m.SetModifierReleaseTracked(true)

	m.CycleWindows(true) // land on C
	if got := activeTitle(m); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}
	m.NotifyModifiersReleased() // all modifiers up: lock in C

	// Committed (C most recent, D second): a fresh Alt-Tab toggles to D.
	m.CycleWindows(true)
	if got := activeTitle(m); got != "D" {
		t.Errorf("after modifier release, Alt-Tab from C = %q, want D", got)
	}
}

// On the TUI (no modifier-release), a cycle step long after the previous one
// starts a new gesture and locks the prior run in first (the idle timer).
func TestCycleWindowsIdleLockInCommitsPriorRun(t *testing.T) {
	m, _ := newFourWindowManager(t)
	// TUI default: modifier release not tracked, so the idle timer is active.

	m.CycleWindows(true) // land on C, cycling
	if got := activeTitle(m); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}

	// Simulate more than the lock-in timeout passing since that step.
	m.mu.Lock()
	m.lastCycleAt = m.lastCycleAt.Add(-2 * cycleCommitTimeout)
	m.mu.Unlock()

	// The next step is a new gesture: it locks C in first, then Alt-Tab from
	// the committed order toggles back to D.
	m.CycleWindows(true)
	if got := activeTitle(m); got != "D" {
		t.Errorf("after idle lock-in, Alt-Tab from C = %q, want D", got)
	}
}

// With modifier-release tracking on (SDL), the idle timer is disabled: a late
// cycle step does NOT lock in the prior run - the run keeps going as one
// gesture until modifiers rise.
func TestCycleWindowsIdleTimerDisabledWhenModifiersTracked(t *testing.T) {
	m, _ := newFourWindowManager(t)
	m.SetModifierReleaseTracked(true)

	m.CycleWindows(true) // land on C
	m.mu.Lock()
	m.lastCycleAt = m.lastCycleAt.Add(-2 * cycleCommitTimeout)
	m.mu.Unlock()

	// No idle lock-in: the run continues from the frozen order [A,B,C,D],
	// forward from C is B (not D, which a committed C would give).
	m.CycleWindows(true)
	if got := activeTitle(m); got != "B" {
		t.Errorf("with modifier tracking, forward from C = %q, want B (no idle commit)", got)
	}
}

// Menu-bar keys must NOT commit the cycle order: a key that the active window
// declines (falling through to the desktop/menu bar) leaves the MRU frozen, so
// the run's landing spot is not promoted.
func TestCycleWindowsMenuKeyDoesNotCommit(t *testing.T) {
	m, _ := newFourWindowManager(t)

	// Alt-Tab to C. The MRU stays [A,B,C,D] (frozen during the run).
	m.CycleWindows(true)
	if got := activeTitle(m); got != "C" {
		t.Fatalf("landed on %q, want C", got)
	}

	// A key the active window declines (a bare window handles no "F9") falls
	// through to the desktop/menu bar and must not commit the MRU.
	m.HandleKeyPress(core.KeyPressEvent{Key: "F9"})

	// MRU uncommitted: a fresh Alt-Tab from C walks the ORIGINAL frozen order
	// [A,B,C,D] -> forward from C is B (not D, which a committed C gives).
	m.CycleWindows(true)
	if got := activeTitle(m); got != "B" {
		t.Errorf("after a menu-bar key, forward from C = %q, want B (MRU uncommitted)", got)
	}
}
