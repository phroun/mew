package editor

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// flipBidiForHost=auto: detect whether the host terminal applies its own bidi
// reordering, once, at the first point it matters — the first frame containing
// RTL content. The probe saves the cursor, prints two Hebrew letters at row 2
// column 1, asks the terminal where its cursor ended up (DSR, ESC[6n), and
// restores; the dirtied row repaints on the next frame. A stream-order
// terminal answers column 3 (two cells advanced); a terminal whose cursor
// moved any other way is applying bidi, so the flip turns on. The reply
// (ESC[row;colR) arrives on stdin — direct-key-handler surfaces it as a
// "CPR:<row>;<col>" key event, which the main loop routes here. If no reply
// arrives (older handler, or a terminal that ignores DSR), a timeout falls
// back to the TERM_PROGRAM environment (Apple_Terminal is the known
// bidi-applying terminal). An explicit flipBidiForHost=true/false skips all of
// this.

const (
	bidiProbeIdle    = 0 // not sent (or not needed)
	bidiProbePending = 1 // sent, awaiting the CPR reply
	bidiProbeDone    = 2 // resolved (reply, timeout, or explicit setting)
)

// bidiProbeExpectCol is the column a stream-order terminal reports after the
// probe prints two 1-column Hebrew letters at column 1.
const bidiProbeExpectCol = 3

// maybeSendBidiProbe fires the one-time terminal probe when auto detection is
// armed and RTL content has appeared on screen. Called after each render.
func (e *Editor) maybeSendBidiProbe() {
	if e.bidiProbeState != bidiProbeIdle || e.Config.FlipBidiForHost != "auto" {
		return
	}
	if !e.realTerminal || !e.Renderer.SawRTLContent() {
		return
	}
	e.bidiProbeState = bidiProbePending
	e.bidiProbeDeadline = time.Now().Add(500 * time.Millisecond)
	// Save cursor, print the probe at row 2 col 1, ask for the cursor
	// position, restore. Row 2 repaints on the next frame.
	e.Renderer.EmitProbe("\x1b7\x1b[2;1Hאב\x1b[6n\x1b8", 2)
	e.RequestRender()
}

// checkBidiProbeTimeout resolves a pending probe from the environment when the
// reply never came. Called from the render path so it runs as events flow.
func (e *Editor) checkBidiProbeTimeout() {
	if e.bidiProbeState != bidiProbePending || time.Now().Before(e.bidiProbeDeadline) {
		return
	}
	e.bidiProbeState = bidiProbeDone
	e.applyBidiProbeResult(envSaysBidiTerminal(), "environment")
}

// handleBidiProbeReply consumes a "CPR:<row>;<col>" key event; reports whether
// the event was a probe reply (consumed) or an ordinary key (process normally).
func (e *Editor) handleBidiProbeReply(key string) bool {
	if !strings.HasPrefix(key, "CPR:") {
		return false
	}
	if e.bidiProbeState != bidiProbePending {
		return true // stray report: swallow it, never type it
	}
	e.bidiProbeState = bidiProbeDone
	col := -1
	if parts := strings.SplitN(strings.TrimPrefix(key, "CPR:"), ";", 2); len(parts) == 2 {
		if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			col = n
		}
	}
	switch {
	case col == bidiProbeExpectCol:
		// The cursor advanced in stream order. Display-only bidi terminals
		// (Terminal.app) still pass this test, so consult the environment.
		e.applyBidiProbeResult(envSaysBidiTerminal(), "probe+environment")
	case col > 0:
		// The cursor moved some other way: the terminal is applying bidi.
		e.applyBidiProbeResult(true, "probe")
	default:
		e.applyBidiProbeResult(envSaysBidiTerminal(), "environment")
	}
	return true
}

// applyBidiProbeResult applies the detected flip mode.
func (e *Editor) applyBidiProbeResult(flip bool, how string) {
	e.Renderer.SetFlipBidiForHost(flip)
	if flip {
		e.ShowNotification("Terminal applies its own bidi (" + how + "): RTL emission flipped")
	}
	e.RequestRender()
}

// envSaysBidiTerminal reports whether the environment identifies a terminal
// known to apply its own bidi reordering.
func envSaysBidiTerminal() bool {
	return os.Getenv("TERM_PROGRAM") == "Apple_Terminal"
}
