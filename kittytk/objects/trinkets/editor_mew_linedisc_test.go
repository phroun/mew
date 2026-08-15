//go:build mew

package trinkets

import "testing"

// A console collects a line and releases it whole. The pipe fallback has to be
// that console, and the bug this replaces was the absence of it: every letter
// went to the child as its own write, and a reader that takes each read as a
// line's worth then sees a command per keystroke.
func TestLineDiscHoldsTheLineUntilEnter(t *testing.T) {
	var d lineDisc

	// Letters echo and send NOTHING. This is the whole point.
	child, echo := d.Feed([]byte("dir"))
	if len(child) != 0 {
		t.Errorf("letters sent %q to the child; nothing should go until Enter", child)
	}
	if string(echo) != "dir" {
		t.Errorf("echo = %q, want dir", echo)
	}

	// Enter releases the line, terminated the way a pipe reader wants.
	child, echo = d.Feed([]byte("\r"))
	if string(child) != "dir\r\n" {
		t.Errorf("child got %q, want the whole line as dir\\r\\n", child)
	}
	if string(echo) != "\r\n" {
		t.Errorf("echo = %q, want a newline", echo)
	}

	// And the buffer is empty again.
	if child, _ := d.Feed([]byte("\r")); string(child) != "\r\n" {
		t.Errorf("second Enter sent %q, want a bare line ending", child)
	}
}

// Backspace erases, which the fallback simply did not do: there is no console
// to do it, so the three bytes that rub a character out have to come from here.
func TestLineDiscBackspace(t *testing.T) {
	var d lineDisc
	d.Feed([]byte("dirx"))

	_, echo := d.Feed([]byte{0x08})
	if string(echo) != "\b \b" {
		t.Errorf("backspace echoed %q, want \\b \\b", echo)
	}
	child, _ := d.Feed([]byte("\r"))
	if string(child) != "dir\r\n" {
		t.Errorf("child got %q, want the x erased", child)
	}

	// Backspace on an empty line erases nothing — and must not walk off the
	// front of the buffer.
	var e lineDisc
	if _, echo := e.Feed([]byte{0x08, 0x7f}); len(echo) != 0 {
		t.Errorf("backspace on an empty line echoed %q, want nothing", echo)
	}
}

// A multi-byte character is erased whole. Removing one byte of a rune would
// leave the line holding half a character and send it to the child that way.
func TestLineDiscBackspaceWholeRune(t *testing.T) {
	var d lineDisc
	d.Feed([]byte("aé")) // é is two bytes
	d.Feed([]byte{0x7f}) // DEL
	child, _ := d.Feed([]byte("\r"))
	if string(child) != "a\r\n" {
		t.Errorf("child got %q, want the whole rune erased", child)
	}
}

// Ctrl-C abandons the line without sending it, as a shell's would.
func TestLineDiscCtrlC(t *testing.T) {
	var d lineDisc
	d.Feed([]byte("format c:"))
	child, echo := d.Feed([]byte{0x03})
	if len(child) != 0 {
		t.Errorf("Ctrl-C sent %q to the child; it should abandon the line", child)
	}
	if string(echo) != "^C\r\n" {
		t.Errorf("echo = %q, want ^C and a newline", echo)
	}
	if child, _ := d.Feed([]byte("\r")); string(child) != "\r\n" {
		t.Errorf("after Ctrl-C the line should be empty, got %q", child)
	}
}

// A pasted line arrives as one write and behaves the same as typing it.
func TestLineDiscHandlesAWholeLineAtOnce(t *testing.T) {
	var d lineDisc
	child, echo := d.Feed([]byte("echo hi\r"))
	if string(child) != "echo hi\r\n" {
		t.Errorf("child got %q", child)
	}
	if string(echo) != "echo hi\r\n" {
		t.Errorf("echo = %q", echo)
	}
}
