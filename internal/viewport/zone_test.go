package viewport

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// makeZoned installs a viewport with an explicit ID (so the ID-sorted cycle
// order is deterministic) in the given set, visible and focusable.
func makeZoned(m *Manager, id, set string, dock DockPosition) *Viewport {
	m.CreateViewport(ViewportOptions{
		ID: id, Type: DocViewport, ViewportSet: set, Dock: dock,
		Buffer: buffer.NewFromString(id + "\n"), Visible: true,
	})
	return m.GetViewport(id)
}

// TestZoneScopedCycleStaysInZone: viewport_next/prior (FocusNext/PrevInZone)
// cycle only within the focused viewport's ViewportSet and never cross into
// another zone.
func TestZoneScopedCycleStaysInZone(t *testing.T) {
	m := NewManager()
	d1 := makeZoned(m, "doc1", "", DockNone)
	d2 := makeZoned(m, "doc2", "", DockNone)
	makeZoned(m, "help1", "help", DockTop)
	makeZoned(m, "help2", "help", DockTop)

	m.SetFocus(d1.ID)

	// Forward within the "" zone: doc1 -> doc2 -> wrap to doc1. Never help.
	if !m.FocusNextInZone() || m.GetFocusedViewport() != d2 {
		t.Fatalf("next-in-zone from doc1 should land on doc2, got %v", focusedID(m))
	}
	if !m.FocusNextInZone() || m.GetFocusedViewport() != d1 {
		t.Fatalf("next-in-zone from doc2 should wrap to doc1, got %v", focusedID(m))
	}
	// Backward wraps the other way, still inside the zone.
	if !m.FocusPrevInZone() || m.GetFocusedViewport() != d2 {
		t.Fatalf("prev-in-zone from doc1 should wrap to doc2, got %v", focusedID(m))
	}
}

// TestZoneJumpAndMemory: zone_next/prior (FocusNext/PrevZone) move to the
// adjacent zone and land on that zone's last-focused viewport, falling back to
// its first visible member when the zone has no focus memory yet.
func TestZoneJumpAndMemory(t *testing.T) {
	m := NewManager()
	d1 := makeZoned(m, "doc1", "", DockNone)
	makeZoned(m, "doc2", "", DockNone)
	h1 := makeZoned(m, "help1", "help", DockTop)
	h2 := makeZoned(m, "help2", "help", DockTop)

	m.SetFocus(d1.ID)

	// No help focused yet -> zone_next falls back to the first visible in "help".
	if !m.FocusNextZone() || m.GetFocusedViewport() != h1 {
		t.Fatalf("zone_next should fall back to help1, got %v", focusedID(m))
	}

	// Establish help2 as the help zone's last-focused, then go back to docs.
	m.SetFocus(h2.ID)
	m.SetFocus(d1.ID)

	// zone_next now restores the help zone's remembered viewport (help2).
	if !m.FocusNextZone() || m.GetFocusedViewport() != h2 {
		t.Fatalf("zone_next should restore last-focused help2, got %v", focusedID(m))
	}
	// From help, jumping back lands on the doc zone's remembered viewport (doc1).
	if !m.FocusPrevZone() || m.GetFocusedViewport() != d1 {
		t.Fatalf("zone_prior from help should restore doc1, got %v", focusedID(m))
	}
}

// TestZoneJumpSingleZoneIsNoop: with only one zone present, a zone jump has
// nowhere to go and reports no change.
func TestZoneJumpSingleZoneIsNoop(t *testing.T) {
	m := NewManager()
	d1 := makeZoned(m, "doc1", "", DockNone)
	makeZoned(m, "doc2", "", DockNone)
	m.SetFocus(d1.ID)

	if m.FocusNextZone() {
		t.Fatalf("zone_next with a single zone should be a no-op, focus=%v", focusedID(m))
	}
	if m.GetFocusedViewport() != d1 {
		t.Fatalf("a no-op zone jump must not move focus, got %v", focusedID(m))
	}
}

func focusedID(m *Manager) string {
	if w := m.GetFocusedViewport(); w != nil {
		return w.ID
	}
	return "<none>"
}
