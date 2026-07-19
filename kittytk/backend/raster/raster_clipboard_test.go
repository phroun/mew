package raster

import "testing"

// Without a system-clipboard bridge the backend uses its internal string
// (headless behavior).
func TestClipboardInternalFallback(t *testing.T) {
	b, err := New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	b.SetClipboard("hello")
	if got := b.GetClipboard(); got != "hello" {
		t.Errorf("internal clipboard = %q, want hello", got)
	}
}

// With a bridge wired (as the SDL host does), Get/Set delegate to the system
// clipboard, while a local copy is still kept as a fallback.
func TestClipboardSystemBridge(t *testing.T) {
	b, err := New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	var sys string
	b.SetSystemClipboard(
		func() string { return sys },
		func(s string) { sys = s },
	)

	b.SetClipboard("copied")
	if sys != "copied" {
		t.Errorf("system clipboard = %q, want copied (Set did not bridge)", sys)
	}
	if got := b.GetClipboard(); got != "copied" {
		t.Errorf("GetClipboard = %q, want copied (Get did not bridge)", got)
	}

	// A change made externally (another app) is observed through the bridge.
	sys = "external"
	if got := b.GetClipboard(); got != "external" {
		t.Errorf("GetClipboard = %q, want external", got)
	}

	// Clearing the bridge reverts to the internal copy kept during SetClipboard.
	b.SetSystemClipboard(nil, nil)
	if got := b.GetClipboard(); got != "copied" {
		t.Errorf("after unbridge, GetClipboard = %q, want copied (local fallback)", got)
	}
}
