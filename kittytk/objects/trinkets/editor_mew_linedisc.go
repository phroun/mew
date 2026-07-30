//go:build mew

package trinkets

// A line discipline for the plain-pipe fallback.
//
// A console does more for a program than carry bytes. It COLLECTS a line —
// echoing as you type, erasing on backspace — and hands the reader the whole
// thing at once when Enter is pressed. Nothing else does that. A pipe delivers
// every write immediately, so typing a letter is a write of one byte, and a
// reader that takes each read as a line's worth (Wine's cmd.exe does; the real
// one is more forgiving) sees a command per keystroke.
//
// That is what "every letter I press sends a newline after it" was. Not a
// stray newline in the send path — one write per letter, and a picky reader
// treating each as a line.
//
// So this is the missing half: accumulate, echo, erase, and write ONE line
// when Enter is pressed. It also gets backspace working, which the fallback
// simply did not have.
//
// It is pure and lives here, apart from any platform, so it can be tested
// where the tests run.

// lineDisc collects typed bytes into a line.
type lineDisc struct {
	line []byte
}

// Feed takes what was typed and returns what the child should receive and what
// should appear on screen. Either may be empty: a letter echoes and sends
// nothing until the line is complete.
func (d *lineDisc) Feed(b []byte) (toChild, echo []byte) {
	for _, c := range b {
		switch c {
		case '\r', '\n':
			// The line is done. A pipe reader wants CRLF; the display wants
			// the cursor on the next line.
			toChild = append(toChild, d.line...)
			toChild = append(toChild, '\r', '\n')
			echo = append(echo, '\r', '\n')
			d.line = d.line[:0]

		case 0x08, 0x7f: // backspace, delete
			if n := trimLastRune(d.line); n > 0 {
				d.line = d.line[:len(d.line)-n]
				// Back over it, paint a space, back again: the three bytes
				// that erase a character on any terminal ever made.
				echo = append(echo, '\b', ' ', '\b')
			}

		case 0x03: // Ctrl-C: abandon the line, as a shell would
			echo = append(echo, '^', 'C', '\r', '\n')
			d.line = d.line[:0]

		default:
			if c < 0x20 && c != '\t' {
				continue // no console here to interpret the rest
			}
			d.line = append(d.line, c)
			echo = append(echo, c)
		}
	}
	return toChild, echo
}

// trimLastRune reports how many bytes the last character occupies, so
// backspace erases a character rather than a byte and a multi-byte one is not
// left half-deleted.
func trimLastRune(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	n := 1
	// Walk back over UTF-8 continuation bytes (10xxxxxx) to the lead byte.
	for n < len(p) && n < 4 && p[len(p)-n]&0xc0 == 0x80 {
		n++
	}
	return n
}
