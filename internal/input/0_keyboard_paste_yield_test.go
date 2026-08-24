package input

import (
	"bytes"
	"testing"
)

// feed queues n paste chunks without a terminal behind them, which is what a
// paste in flight looks like to GetEvent.
func feed(kh *KeyboardHandler, n int) {
	for i := 0; i < n; i++ {
		kh.PasteChunks <- PasteChunk{Content: []byte("x"), IsFinal: i == n-1}
	}
}

// A paste in flight cannot lock out the keyboard.
//
// The priority pass used to be absolute, and a large paste keeps the channel
// non-empty — the main loop renders and syncs per chunk, so it drains slower
// than a terminal delivers. Nothing else was served until the paste finished,
// keys piled up in the 256 direct-key-handler holds, and it drops the OLDEST to
// make room: a long enough paste silently discarded what had been typed behind
// it, so Escape during a paste might never arrive at all.
func TestAPasteYieldsToAWaitingKey(t *testing.T) {
	kh := NewKeyboardHandler(bytes.NewReader(nil), &bytes.Buffer{})
	feed(kh, pasteRunBeforeYield*4)
	kh.handler.Keys <- "Escape"

	for i := 0; i < pasteRunBeforeYield*2; i++ {
		if ev := kh.GetEvent(); ev.Key == "Escape" {
			return
		}
	}
	t.Fatalf("the Escape never arrived in %d events with a paste in flight; "+
		"a key behind a long enough paste is dropped rather than delayed",
		pasteRunBeforeYield*2)
}

// The yield drains the whole backlog before going back to the paste, which is
// what keeps one from outrunning the 256 events the key channel holds.
func TestTheYieldDrainsTheKeysWaitingBehindThePaste(t *testing.T) {
	kh := NewKeyboardHandler(bytes.NewReader(nil), &bytes.Buffer{})
	feed(kh, pasteRunBeforeYield*4)
	for _, k := range []string{"a", "b", "c"} {
		kh.handler.Keys <- k
	}

	var keys []string
	for i := 0; i < pasteRunBeforeYield*3 && len(keys) < 3; i++ {
		if ev := kh.GetEvent(); ev.Key != "" {
			keys = append(keys, ev.Key)
		}
	}
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("drained %v, want [a b c] together once the run was spent", keys)
	}
}

// And the paste still has priority in the ordinary case: nothing waiting means
// it runs on without interruption.
func TestAPasteWithNothingWaitingRunsStraightThrough(t *testing.T) {
	kh := NewKeyboardHandler(bytes.NewReader(nil), &bytes.Buffer{})
	n := pasteRunBeforeYield * 3
	feed(kh, n)

	for i := 0; i < n; i++ {
		if ev := kh.GetEvent(); ev.Paste == nil {
			t.Fatalf("event %d was %+v, want a paste chunk; with nothing else "+
				"waiting the paste should not be interrupted", i, ev)
		}
	}
}

// A posted action is served by the yield too. It is how the OS clipboard reply
// comes back, and it waited behind the paste the same way.
func TestTheYieldServesAPostedAction(t *testing.T) {
	kh := NewKeyboardHandler(bytes.NewReader(nil), &bytes.Buffer{})
	feed(kh, pasteRunBeforeYield*4)
	kh.PostAction(func() {})

	for i := 0; i < pasteRunBeforeYield*2; i++ {
		if ev := kh.GetEvent(); ev.Do != nil {
			return
		}
	}
	t.Fatal("the posted action never arrived with a paste in flight")
}
