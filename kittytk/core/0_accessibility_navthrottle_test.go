package core

import (
	"testing"
	"time"
)

// Navigation announcements throttle speech: the first speaks, rapid
// follow-ups only show (non-vocal) and hold a pending, and after a pause
// the settled-on item speaks. Every one is still emitted (status bar).
func TestNavigationAnnouncementThrottle(t *testing.T) {
	am := NewAccessibilityManager()
	base := time.Unix(1000, 0)
	now := base
	am.nowFn = func() time.Time { return now }

	var got []AccessibilityAnnouncement
	am.OnAnnounce = func(a AccessibilityAnnouncement) { got = append(got, a) }

	// First of a burst: emitted and vocal.
	am.AnnounceNavigation("item 1")
	if len(got) != 1 || !got[0].Vocal || got[0].Message != "item 1" {
		t.Fatalf("first = %+v", got)
	}

	// Within the window: emitted for the status bar, but not vocal.
	now = base.Add(100 * time.Millisecond)
	am.AnnounceNavigation("item 2")
	if len(got) != 2 || got[1].Vocal || got[1].Message != "item 2" {
		t.Fatalf("second = %+v", got)
	}

	// Another quick move overwrites the pending, still non-vocal.
	now = base.Add(200 * time.Millisecond)
	am.AnnounceNavigation("item 3")
	if len(got) != 3 || got[2].Vocal {
		t.Fatalf("third = %+v", got)
	}

	// Before the quiet window elapses (deadline 700ms): no flush.
	now = base.Add(600 * time.Millisecond)
	am.ProcessPending()
	if len(got) != 3 {
		t.Fatalf("early flush emitted %+v", got)
	}

	// After the pause: the last held item speaks.
	now = base.Add(701 * time.Millisecond)
	am.ProcessPending()
	if len(got) != 4 || !got[3].Vocal || got[3].Message != "item 3" {
		t.Fatalf("flush = %+v", got)
	}
	// Idempotent: nothing left to flush.
	am.ProcessPending()
	if len(got) != 4 {
		t.Fatalf("double flush = %+v", got)
	}

	// A move after a clear pause is immediate/vocal again.
	now = base.Add(1500 * time.Millisecond)
	am.AnnounceNavigation("item 4")
	if len(got) != 5 || !got[4].Vocal {
		t.Fatalf("next burst = %+v", got)
	}
}
