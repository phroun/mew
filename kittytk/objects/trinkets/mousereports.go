package trinkets

// Classifying what a terminal is being SENT.
//
// Both the standalone and the hosted paths need to tell typed input from
// mouse reports, and motion from the rest, so the rules live here rather than
// behind a build tag: an editor surface encodes mouse MOVES and sends them on
// exactly as a terminal does, and treating those as user action is what pins
// a caret permanently lit.

// isMouseReport reports whether p is any xterm mouse report — press, release,
// wheel or motion — as opposed to typed input.
func isMouseReport(p []byte) bool {
	if len(p) < 3 || p[0] != 0x1b || p[1] != '[' {
		return false
	}
	return p[2] == '<' || (p[2] == 'M' && len(p) >= 6)
}

// isMouseMotionReport reports whether p is an xterm mouse report carrying the
// MOTION flag (bit 5, decimal 32) — the stream a pointer drag produces.
//
// SGR (DECSET 1006): ESC [ < Nb ; Cx ; Cy (M|m)
// X10/normal:        ESC [ M Cb Cx Cy, with Cb biased by 32
func isMouseMotionReport(p []byte) bool {
	if len(p) < 3 || p[0] != 0x1b || p[1] != '[' {
		return false
	}
	if p[2] == '<' { // SGR
		nb, i := 0, 3
		for i < len(p) && p[i] >= '0' && p[i] <= '9' {
			nb = nb*10 + int(p[i]-'0')
			i++
		}
		if i >= len(p) || p[i] != ';' {
			return false
		}
		return nb&32 != 0
	}
	if p[2] == 'M' && len(p) >= 6 { // X10: button byte is biased by 32
		return (int(p[3])-32)&32 != 0
	}
	return false
}
