package mew

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// runFeedSession drives a full editor session through a host-fed KeyFeed
// with a virtual terminal, returning the final document content.
func runFeedSession(t *testing.T, content string, drive func(f *KeyFeed)) string {
	t.Helper()
	feed := NewKeyFeed()
	var out syncWriter
	done := make(chan struct{})
	var result string
	var err error
	go func() {
		defer close(done)
		result, err = EditContent(content,
			WithoutUserConfig(),
			WithoutProfileScript(),
			WithColdStoragePath(t.TempDir()),
			WithKeyFeed(feed),
			WithTerminal(Terminal{
				Output: &out,
				Size:   func() (int, int, error) { return 80, 24, nil },
			}),
		)
	}()
	drive(feed)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
	}
	if err != nil {
		t.Fatalf("EditContent: %v", err)
	}
	return result
}

// Typing keys through the feed edits the document; closing the feed ends it.
func TestKeyFeedTyping(t *testing.T) {
	got := runFeedSession(t, "world\n", func(f *KeyFeed) {
		for _, k := range []string{"h", "i", " "} {
			if !f.SendKey(k) {
				t.Error("SendKey reported closed feed")
			}
		}
		f.SendKey("Enter") // direct-key-handler naming, normalized to return
		f.Close()
	})
	if got != "hi \nworld\n" {
		t.Fatalf("content: %q", got)
	}
}

// Paste chunks through the feed are inserted verbatim, never re-parsed as
// keys, and a multi-chunk paste arrives in order.
func TestKeyFeedPaste(t *testing.T) {
	got := runFeedSession(t, "\n", func(f *KeyFeed) {
		f.SendPaste([]byte("no ^C here "), false)
		f.SendPaste([]byte("and no esc"), true)
		f.Close()
	})
	if got != "no ^C here and no esc\n" {
		t.Fatalf("content: %q", got)
	}
}

// ^C on an unmodified last buffer runs cancel|buffer_close: cancel is a no-op
// (no prompt), so buffer_close runs and, as the last buffer, exits the editor.
func TestKeyFeedCtrlCExitsUnmodified(t *testing.T) {
	got := runFeedSession(t, "hello\n", func(f *KeyFeed) {
		f.SendKey("^C")
	})
	if got != "hello\n" {
		t.Fatalf("content: %q", got)
	}
}

// A host app can hand mew a raw argument STRING (EditArgs): it parses the
// command line, opens the named file, and runs a normal session.
func TestEditArgsHostString(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	feed := NewKeyFeed()
	var out syncWriter
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		err = EditArgs("--showLineNumbers- "+path,
			WithoutUserConfig(),
			WithoutProfileScript(),
			WithColdStoragePath(t.TempDir()),
			WithKeyFeed(feed),
			WithTerminal(Terminal{
				Output: &out,
				Size:   func() (int, int, error) { return 80, 24, nil },
			}),
		)
	}()
	feed.Close() // end the session immediately
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
	}
	if err != nil {
		t.Fatalf("EditArgs: %v", err)
	}
}

// Closing the feed ends the session cleanly, delivering queued events first.
func TestKeyFeedClose(t *testing.T) {
	got := runFeedSession(t, "\n", func(f *KeyFeed) {
		f.SendKey("x")
		f.Close()
		if f.SendKey("y") {
			t.Error("SendKey should report false after Close")
		}
	})
	if got != "x\n" {
		t.Fatalf("content: %q", got)
	}
}

// A key sequence (^K F) reaches its mapped command through the feed: the
// find prompt opens, and cancelling then closing the feed leaves content intact.
func TestKeyFeedKeySequence(t *testing.T) {
	got := runFeedSession(t, "hello\n", func(f *KeyFeed) {
		f.SendKey("^K")
		f.SendKey("F")  // find prompt opens
		f.SendKey("^C") // cancel the prompt (cancel|buffer_close cancels first)
		f.Close()       // end the session
	})
	if got != "hello\n" {
		t.Fatalf("content: %q", got)
	}
}
