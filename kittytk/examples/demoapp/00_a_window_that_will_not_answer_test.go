package main

// The Sulking Window: a window whose application is consulted about closing it and
// never answers, so the display's "did not respond — force close?" question can be
// watched happening.
//
// These check the wiring, which is the part that can silently fall off: a menu item
// naming an action nothing listens for, or a subscription that was never made -- and
// a window that closes normally looks exactly like one whose demonstration is broken.

import (
	"os"
	"strings"
	"testing"
)

func TestTheSulkingWindowIsReachable(t *testing.T) {
	script := mainBuildScript()
	if !strings.Contains(script, "action=demo.file.sulking") {
		t.Error("no menu item names demo.file.sulking, so there is no way to open it")
	}

	src, err := os.ReadFile("wire.go")
	if err != nil {
		t.Fatalf("reading wire.go: %v", err)
	}
	if !strings.Contains(string(src), "a.wireSulking(c)") {
		t.Error("wireSulking is never called, so the menu item fires nothing")
	}

	src, err = os.ReadFile("closing.go")
	if err != nil {
		t.Fatalf("reading closing.go: %v", err)
	}
	if !strings.Contains(string(src), `OnCommand("demo.file.sulking"`) {
		t.Error("nothing listens for demo.file.sulking")
	}
	// **Subscribed is what makes the close askable at all.** Without this the
	// window closes at once and the demonstration is of nothing.
	if !strings.Contains(string(src), `win.On("window_closing"`) {
		t.Error("the window's close is not subscribed to, so it closes at once and asks nobody")
	}
	// And NOT answered: a Decide call here would close the window and the force-close
	// question would never come up.
	if strings.Contains(string(src), "Decide(") {
		t.Error("the sulking window answers the question, so it never times out")
	}
}

// The window says what to do with it. A window whose entire behaviour is a
// five-second silence explains nothing by looking at it.
func TestTheSulkingWindowSaysWhatToDoWithIt(t *testing.T) {
	script := sulkingWindowScript(1)
	for _, want := range []string{
		"Sulking Window", // the title the display's dialog will name
		"[x]",            // which way asks
		"destroy",        // and which way does not
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the window does not mention %q", want)
		}
	}
	// Surfaced under the names the wiring uses, or none of it is reachable.
	for _, want := range []string{"swin=", "swcloser="} {
		if !strings.Contains(script, want) {
			t.Errorf("the build surfaces no %s", want)
		}
	}
}

// Each one is its own window, so opening two does not collide on a key and leave
// the second unreachable.
func TestEachSulkingWindowIsItsOwn(t *testing.T) {
	first, second := sulkingWindowScript(1), sulkingWindowScript(2)
	if first == second {
		t.Fatal("two sulking windows are built by the same script, so they share their keys")
	}
	if !strings.Contains(first, "sw1=new window") || !strings.Contains(second, "sw2=new window") {
		t.Error("the windows are not keyed apart")
	}
}
