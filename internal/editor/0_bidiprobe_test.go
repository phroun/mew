package editor

import (
	"strings"
	"testing"
	"time"
)

// probeArmed renders RTL content on an editor pretending to be a real
// terminal, arming and sending the probe.
func probeArmed(t *testing.T) (*Editor, *strings.Builder) {
	t.Helper()
	e, _, out := newRenderedEditor(t, "שלום\n")
	e.realTerminal = true // tests virtualize the terminal; pretend it is real
	var _ = out
	e.performRender() // paints RTL -> sends the probe
	if e.bidiProbeState != bidiProbePending {
		t.Fatalf("probe should be pending after RTL render, state %d", e.bidiProbeState)
	}
	return e, nil
}

// The probe fires once when RTL content first renders, and the emitted stream
// contains the probe sequence (save cursor, write, DSR, restore).
func TestBidiProbeSentOnFirstRTL(t *testing.T) {
	e, _, out := newRenderedEditor(t, "שלום\n")
	e.realTerminal = true
	e.performRender()
	if e.bidiProbeState != bidiProbePending {
		t.Fatalf("probe should be pending, state %d", e.bidiProbeState)
	}
	raw := out.String()
	if !strings.Contains(raw, "\x1b[6n") {
		t.Error("probe should ask for the cursor position (DSR)")
	}
	if !strings.Contains(raw, "\x1b7") || !strings.Contains(raw, "\x1b8") {
		t.Error("probe should save and restore the cursor")
	}
	// A second render must not re-send.
	before := e.bidiProbeState
	e.performRender()
	if e.bidiProbeState != before {
		t.Error("probe must fire only once")
	}
}

// An LTR-only session never probes.
func TestBidiProbeNotSentForLTR(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "plain ascii\n")
	e.realTerminal = true
	e.performRender()
	if e.bidiProbeState != bidiProbeIdle {
		t.Errorf("no RTL content: probe should stay idle, state %d", e.bidiProbeState)
	}
}

// A CPR reply with the stream-order column resolves without enabling the flip
// (no bidi cursor movement; environment is not Apple_Terminal in tests).
func TestBidiProbeReplyStreamOrder(t *testing.T) {
	e, _ := probeArmed(t)
	if !e.handleBidiProbeReply("CPR:2;3") {
		t.Fatal("CPR event should be consumed")
	}
	if e.bidiProbeState != bidiProbeDone {
		t.Error("reply should resolve the probe")
	}
}

// A CPR reply with any other column means the terminal moved the cursor
// bidi-style: the flip turns on.
func TestBidiProbeReplyBidiCursor(t *testing.T) {
	e, _ := probeArmed(t)
	if !e.handleBidiProbeReply("CPR:2;1") {
		t.Fatal("CPR event should be consumed")
	}
	if e.bidiProbeState != bidiProbeDone {
		t.Error("reply should resolve the probe")
	}
	// The flip is now active: a full repaint emits the run in logical order.
	e.Renderer.ForceRedraw()
	e.performRender()
}

// An ordinary key is not consumed by the probe path.
func TestBidiProbeIgnoresOrdinaryKeys(t *testing.T) {
	e, _ := probeArmed(t)
	if e.handleBidiProbeReply("a") {
		t.Error("ordinary keys must pass through")
	}
	if e.handleBidiProbeReply("^X") {
		t.Error("control keys must pass through")
	}
}

// A stray CPR arriving with no probe outstanding is swallowed, never typed.
func TestBidiProbeStrayCPRSwallowed(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "plain\n")
	if !e.handleBidiProbeReply("CPR:5;7") {
		t.Error("a stray CPR must be swallowed (never typed into the buffer)")
	}
}

// The timeout path resolves a pending probe.
func TestBidiProbeTimeout(t *testing.T) {
	e, _ := probeArmed(t)
	e.bidiProbeDeadline = time.Now().Add(-time.Second)
	e.checkBidiProbeTimeout()
	if e.bidiProbeState != bidiProbeDone {
		t.Error("timeout should resolve the probe")
	}
}

// An explicit setting disarms auto detection.
func TestBidiProbeExplicitSettingWins(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "שלום\n")
	e.realTerminal = true
	e.setOption(nil, "flipbidiforhost", "false")
	e.performRender()
	if e.bidiProbeState != bidiProbeDone {
		t.Errorf("explicit setting should disarm the probe, state %d", e.bidiProbeState)
	}
}
