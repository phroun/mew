package editor

import (
	"testing"
)

// A plain key completes its fallthrough insert binding, then the viewport's
// after-key pseudo-binding runs — after, so its effect lands second.
func TestAfterKeyFiresOnPlainKey(t *testing.T) {
	e, w := newTestEditor(t, "")
	w.AfterKey = "insert '!'"

	e.dispatchKey("y")

	if got := docContent(w); got != "y!" {
		t.Fatalf("content = %q, want %q (binding first, hook second)", got, "y!")
	}
}

// A prefix key silently pending a sequence does NOT fire the hook; completing
// the sequence fires it exactly once, after the bound command.
func TestAfterKeySilentOnPrefixFiresOnCompletion(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.KeyProcessor.MapKey("F35 a", "go_line_start")
	w.AfterKey = "insert '!'"

	e.dispatchKey("F35")
	if got := docContent(w); got != "" {
		t.Fatalf("content after prefix = %q, want empty (hook must not fire mid-sequence)", got)
	}
	if e.KeyProcessor.GetActiveSequence() == "" {
		t.Fatal("F35 should be pending as a sequence prefix")
	}

	e.dispatchKey("a")
	if got := docContent(w); got != "!" {
		t.Fatalf("content after completion = %q, want %q (one hook firing)", got, "!")
	}
}

// A non-matching sequence unwinds — its keys replay as ordinary bindings —
// and the hook fires as soon as the first of them resolves.
func TestAfterKeyFiresOnSequenceUnwind(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.KeyProcessor.MapKey("F35 a", "go_line_start")
	w.AfterKey = "insert '!'"

	e.dispatchKey("F35")
	e.dispatchKey("q") // F35 q matches nothing: unwind replays q as insert

	if got := docContent(w); got != "q!" {
		t.Fatalf("content = %q, want %q (unwound insert, then hook)", got, "q!")
	}
}

// When the unwind replays SEVERAL keys, the hook fires right after the FIRST
// one lands as a normal press — it does not wait for the rest of the unwind.
func TestAfterKeyFiresOnFirstUnwoundKey(t *testing.T) {
	e, w := newTestEditor(t, "")
	// "j" becomes a sequence starter; "j x" matches nothing, so the unwind
	// first inserts the held-back "j", then replays "x" as an ordinary insert.
	e.KeyProcessor.MapKey("j k", "go_line_start")
	w.AfterKey = "insert '!'"

	e.dispatchKey("j") // silent prefix — no hook
	if got := docContent(w); got != "" {
		t.Fatalf("content after prefix = %q, want empty", got)
	}
	e.dispatchKey("x")

	if got := docContent(w); got != "j!x" {
		t.Fatalf("content = %q, want %q (hook right after the first unwound key)", got, "j!x")
	}
}

// A key that resolves to no command at all (an unmapped named key) is not a
// completed binding: the hook stays quiet.
func TestAfterKeyQuietWhenNothingExecutes(t *testing.T) {
	e, w := newTestEditor(t, "")
	w.AfterKey = "insert '!'"

	e.dispatchKey("F30")

	if got := docContent(w); got != "" {
		t.Fatalf("content = %q, want empty (no binding resolved)", got)
	}
}

// The motivating shape: a prompt viewport with AfterKey="accept" turns into a
// single-keystroke prompt — pressing y falls through to insert 'y', then the
// hook accepts the prompt with that answer.
func TestAfterKeyAutoAcceptPrompt(t *testing.T) {
	e, _ := newTestEditor(t, "")
	var got string
	accepted := false
	e.PromptMgr.PromptForInput("Really? ", "", func(ok bool, content, _ string) {
		accepted = ok
		got = content
	}, "")
	fp := focusedPrompt(e)
	if fp == nil {
		t.Fatal("prompt should be focused")
	}
	fp.AfterKey = "accept"

	e.dispatchKey("y")

	if !accepted || got != "y" {
		t.Fatalf("prompt result = (%v, %q), want accepted %q", accepted, got, "y")
	}
	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed after the auto-accept")
	}
}

// The after_key command sets and clears the focused viewport's pseudo-binding
// from script.
func TestAfterKeyCommand(t *testing.T) {
	e, w := newTestEditor(t, "")

	e.executeCommand(`after_key "go_line_start"`)
	if w.AfterKey != "go_line_start" {
		t.Fatalf("AfterKey = %q, want go_line_start", w.AfterKey)
	}
	e.executeCommand("after_key")
	if w.AfterKey != "" {
		t.Fatalf("AfterKey = %q, want cleared", w.AfterKey)
	}
}

// A binding that closes its own viewport (the prompt's accept) must not run
// the departed viewport's hook afterwards.
func TestAfterKeyNotAfterViewportCloses(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.PromptMgr.PromptForInput("Q? ", "", func(bool, string, string) {}, "")
	fp := focusedPrompt(e)
	if fp == nil {
		t.Fatal("prompt should be focused")
	}
	// The hook would type into whatever holds focus after the prompt closes;
	// it must never run because return's accept closed the prompt viewport.
	fp.AfterKey = "insert 'LEAK'"

	e.dispatchKey("return")

	if got := docContent(w); got != "" {
		t.Fatalf("doc content = %q, want empty (hook must die with its viewport)", got)
	}
}
