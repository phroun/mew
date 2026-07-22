package input

import "io"

// Horizontal-wheel plucking.
//
// SGR mouse reports encode the wheel as buttons 64 (up), 65 (down), 66 (left)
// and 67 (right). The key decoder (direct-key-handler) only distinguishes up
// and down; it collapses 66/67 onto "MouseScrollDown", so a sideways gesture
// would scroll the view DOWN. We therefore pull the horizontal reports out of
// the raw byte stream before the decoder sees them and surface each as a
// synthetic "MouseScrollLeft"/"MouseScrollRight" key event. Everything else —
// vertical wheel, clicks, keys, pastes — passes through byte-for-byte.

// splitHScroll scans carry+data for horizontal-wheel SGR mouse reports
// (ESC [ < Cb ; Cx ; Cy (M|m), with Cb a wheel-left/right button) and removes
// them, returning the passthrough bytes, the synthetic event names in order,
// and a trailing partial SGR sequence to prepend on the next call. Only
// complete, well-formed SGR mouse reports are touched; any other use of ESC is
// passed through untouched.
func splitHScroll(carry, data []byte) (out []byte, events []string, newCarry []byte) {
	buf := data
	if len(carry) > 0 {
		buf = append(append([]byte(nil), carry...), data...)
	}
	for i := 0; i < len(buf); {
		if buf[i] != 0x1b {
			out = append(out, buf[i])
			i++
			continue
		}
		// A potential SGR mouse report starts "ESC [ <".
		if !hasPrefixAt(buf, i, "\x1b[<") {
			// Not our sequence, or not enough bytes yet to tell. If the tail is
			// a strict prefix of "\x1b[<", carry it for next time; else emit ESC.
			if isPartialPrefix(buf[i:], "\x1b[<") {
				newCarry = append([]byte(nil), buf[i:]...)
				return out, events, newCarry
			}
			out = append(out, buf[i])
			i++
			continue
		}
		// Find the SGR terminator (M or m).
		term := -1
		for j := i + 3; j < len(buf); j++ {
			c := buf[j]
			if c == 'M' || c == 'm' {
				term = j
				break
			}
			if (c < '0' || c > '9') && c != ';' {
				break // malformed: not an SGR mouse body
			}
		}
		if term < 0 {
			// Incomplete (or malformed-so-far): if it could still complete,
			// carry it; otherwise pass the ESC through and move on.
			if couldCompleteSGR(buf[i:]) {
				newCarry = append([]byte(nil), buf[i:]...)
				return out, events, newCarry
			}
			out = append(out, buf[i])
			i++
			continue
		}
		// Parse "Cb;Cx;Cy" and decide.
		if cb, ok := firstNumber(buf[i+3 : term]); ok && isHWheel(cb) {
			if cb&1 == 0 { // 66 = left, 67 = right
				events = append(events, "MouseScrollLeft")
			} else {
				events = append(events, "MouseScrollRight")
			}
			i = term + 1 // strip the whole report
			continue
		}
		// A non-horizontal SGR mouse report: pass it through verbatim.
		out = append(out, buf[i:term+1]...)
		i = term + 1
	}
	return out, events, nil
}

// isHWheel reports whether an SGR button code is a horizontal wheel (66/67):
// the wheel bit (64) set and the low two bits selecting left(2)/right(3).
func isHWheel(cb int) bool { return cb&64 != 0 && (cb&3 == 2 || cb&3 == 3) }

func hasPrefixAt(b []byte, i int, s string) bool {
	if i+len(s) > len(b) {
		return false
	}
	for k := 0; k < len(s); k++ {
		if b[i+k] != s[k] {
			return false
		}
	}
	return true
}

// isPartialPrefix reports whether b is a non-empty strict prefix of s.
func isPartialPrefix(b []byte, s string) bool {
	if len(b) == 0 || len(b) >= len(s) {
		return false
	}
	for k := range b {
		if b[k] != s[k] {
			return false
		}
	}
	return true
}

// couldCompleteSGR reports whether b (starting "\x1b[<") is a valid, still-open
// SGR mouse body — only digits and ';' after the prefix, no terminator yet.
func couldCompleteSGR(b []byte) bool {
	if !hasPrefixAt(b, 0, "\x1b[<") {
		return false
	}
	for _, c := range b[3:] {
		if c == 'M' || c == 'm' {
			return false // it is complete, not partial
		}
		if (c < '0' || c > '9') && c != ';' {
			return false // malformed
		}
	}
	return true
}

// firstNumber parses the leading decimal number of an SGR body ("Cb;Cx;Cy").
func firstNumber(b []byte) (int, bool) {
	n, any := 0, false
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		any = true
	}
	return n, any
}

// hscrollReader wraps the raw input reader, applying splitHScroll and pushing
// synthetic horizontal-scroll key names to synth. Passthrough bytes are
// buffered and returned from Read like an ordinary reader.
type hscrollReader struct {
	src     io.Reader
	synth   chan<- string
	carry   []byte
	out     []byte
	scratch []byte
}

func newHScrollReader(src io.Reader, synth chan<- string) *hscrollReader {
	return &hscrollReader{src: src, synth: synth, scratch: make([]byte, 4096)}
}

func (r *hscrollReader) Read(p []byte) (int, error) {
	for len(r.out) == 0 {
		n, err := r.src.Read(r.scratch)
		if n > 0 {
			out, events, carry := splitHScroll(r.carry, r.scratch[:n])
			r.carry = carry
			r.out = append(r.out, out...)
			for _, e := range events {
				select {
				case r.synth <- e:
				default: // synth channel full: drop rather than stall input
				}
			}
		}
		if err != nil {
			if len(r.out) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, r.out)
	r.out = r.out[n:]
	return n, nil
}
