//go:build mew

package trinkets

import (
	mew "github.com/phroun/mew"
)

// Waking a HOSTED terminal's caret.
//
// A terminal the host draws for mew never sees its own input: mew owns the
// session and writes to it directly (a tinput, an encoded key, a mouse
// report), then feeds this side the resulting stream. So the trinket's own
// input path — toChild, which wakes the caret for a standalone terminal —
// is never reached, and a caret caught in its dark half stays dark while the
// user types.
//
// The session's WRITE is the honest signal: it happens when, and only when,
// something is sent to the child. Feeding is not a substitute — a host
// re-renders for its own reasons, a mouse move among them, and waking on that
// pins the caret permanently lit.

// blinkWakingSession wraps a session so every write wakes the caret. Two
// shapes exist because PTYExitStatus is OPTIONAL: a wrapper that always
// implemented it would claim an answer for hosts that have none, so the
// wrapper is chosen to match what the inner session actually offers.
type blinkWakingSession struct {
	mew.PTYSession
	wake func(p []byte)
}

func (s blinkWakingSession) Write(p []byte) (int, error) {
	s.wake(p)
	return s.PTYSession.Write(p)
}

// blinkWakingSessionWithExit is blinkWakingSession for a session that also
// reports its child's exit status.
type blinkWakingSessionWithExit struct {
	blinkWakingSession
	exit mew.PTYExitStatus
}

func (s blinkWakingSessionWithExit) ExitStatus() (int, bool) { return s.exit.ExitStatus() }

// wakeOnWrite wraps sess so writes to the child wake this editor's hosted
// carets, preserving an exit-status readout when the session has one.
func (e *Editor) wakeOnWrite(sess mew.PTYSession) mew.PTYSession {
	if sess == nil {
		return nil
	}
	w := blinkWakingSession{PTYSession: sess, wake: e.wakeHostedCarets}
	if ex, ok := sess.(mew.PTYExitStatus); ok {
		return blinkWakingSessionWithExit{blinkWakingSession: w, exit: ex}
	}
	return w
}

// wakeHostedCarets answers a write to the child, and the answer depends on
// what the write IS. Two different questions, two different filters:
//
//   - Wake the caret? For anything the user did on purpose — typing, a click,
//     a wheel tick. NOT for mouse MOTION: an application tracking motion
//     (DECSET 1003, or 1002 during a drag) gets a report for every step the
//     pointer takes, and waking on those pins the caret lit exactly as
//     feeding on every render did.
//
//   - Snap back to the live edge? Only for TYPING. Someone reading scrollback
//     who starts typing means to be at the prompt, so the view returns there
//     — the same reflex every terminal has. But a wheel tick is how they got
//     up there, and snapping on it would make scrolling back impossible;
//     clicks select text up there and must leave the view alone. Terminal
//     OUTPUT never reaches here at all, so watching a build scroll by cannot
//     drag the view down either.
func (e *Editor) wakeHostedCarets(p []byte) {
	mouse := isMouseReport(p)
	if mouse && isMouseMotionReport(p) {
		return
	}
	typed := !mouse
	e.termMu.Lock()
	terms := make([]*PurfecTerm, 0, len(e.termSurfaces))
	for _, s := range e.termSurfaces {
		if s.term != nil {
			terms = append(terms, s.term)
		}
	}
	e.termMu.Unlock()
	for _, t := range terms {
		t.resetCursorBlink()
		if typed {
			t.ScrollToBottom()
		}
	}
}
