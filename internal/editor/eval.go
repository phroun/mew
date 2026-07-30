package editor

import (
	"io"
	"strings"
	"sync"

	"github.com/phroun/pawscript"
)

// Launch evals: each --eval="script" on the command line queues its script
// against the file(s) that follow it (see cli.go for the sticky walk). The
// scripts run IN the live session — visually, one at a time, in file order —
// exactly like a keybound command, so they may open prompts, sleep, or suspend
// on async tokens. While the batch runs, PawScript's #out/#err output diverts
// from its normal sinks (insert-at-cursor / error toast) into an ordered
// capture log. When the batch finishes:
//
//   - session still running: a non-blank log dumps into one fresh buffer
//     (focused), a blank log is dropped and the launch focus is restored;
//   - a script called exit (or the session ended mid-batch): the log is
//     written to the REAL stdout/stderr in occurrence order, after serve has
//     restored the terminal (see RunArgs).

// launchEval is one queued script: the viewport of the file it was declared
// for, and the PawScript to run against it.
type launchEval struct {
	winID  string
	script string
}

// evalChunk is one contiguous write to #out or #err during the batch.
type evalChunk struct {
	err  bool
	text string
}

// evalCapture is the ordered #out/#err log for the launch-eval batch. active
// is flipped on for the batch's whole lifetime (async waits included), so the
// PawScript writers divert here instead of their normal sinks.
type evalCapture struct {
	mu     sync.Mutex
	active bool
	chunks []evalChunk
}

// evalCaptureWrite diverts one PawScript output write into the capture log.
// It reports false when capture is off, leaving the write to the caller's
// normal sink.
func (e *Editor) evalCaptureWrite(isErr bool, p []byte) bool {
	c := &e.evalCap
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return false
	}
	if len(p) == 0 {
		return true
	}
	// Coalesce runs on the same stream so the log stays one chunk per burst.
	if n := len(c.chunks); n > 0 && c.chunks[n-1].err == isErr {
		c.chunks[n-1].text += string(p)
	} else {
		c.chunks = append(c.chunks, evalChunk{err: isErr, text: string(p)})
	}
	return true
}

func (e *Editor) evalCaptureOn() { e.evalCap.mu.Lock(); e.evalCap.active = true; e.evalCap.mu.Unlock() }
func (e *Editor) evalCaptureOff() {
	e.evalCap.mu.Lock()
	e.evalCap.active = false
	e.evalCap.mu.Unlock()
}

// evalCapturedText returns the whole log, streams interleaved in occurrence
// order.
func (e *Editor) evalCapturedText() string {
	e.evalCap.mu.Lock()
	defer e.evalCap.mu.Unlock()
	var b strings.Builder
	for _, c := range e.evalCap.chunks {
		b.WriteString(c.text)
	}
	return b.String()
}

// startLaunchEvals kicks off the queued launch-eval batch. Called from serve
// once, after the initial render, so the scripts run visually; the steps
// themselves execute on the main event loop (each PostAction runs under the
// same lock-and-render tail a keystroke gets).
func (e *Editor) startLaunchEvals() {
	if len(e.launchEvals) == 0 {
		return
	}
	if !e.PostAction(func() { e.continueLaunchEvals(0) }) {
		// No action port (input already closed): run the batch inline; the
		// event loop has not started, so the main goroutine is safe.
		e.continueLaunchEvals(0)
	}
}

// continueLaunchEvals runs eval steps from i, inline while each completes
// synchronously. A script that suspends on an async token parks the batch; a
// watcher goroutine resumes it at the next index when the token resolves.
// Runs on the main event loop (or a test caller standing in for it).
func (e *Editor) continueLaunchEvals(i int) {
	if i == 0 {
		// The batch owns focus while it runs; remember the launch's choice so
		// a blank-output batch can put it back.
		if w := e.ViewportManager.GetFocusedViewport(); w != nil {
			e.evalFocusRestore = w.ID
		}
		e.evalCaptureOn()
	}
	for e.Running && i < len(e.launchEvals) {
		le := e.launchEvals[i]
		// Run the script against its own file: focus that viewport first (it
		// may be gone — a prior script may have closed it — then the script
		// runs wherever focus stands).
		if w := e.ViewportManager.GetViewport(le.winID); w != nil {
			e.ViewportManager.SetFocus(le.winID)
			e.ensureCursorVisible(w)
		}
		res := e.executeCommandResult(le.script)
		if tok, ok := res.(pawscript.TokenResult); ok {
			next := i + 1
			go func() {
				e.PawScript.WaitForToken(string(tok))
				// If the post fails the session is over; the capture log
				// stays for RunArgs to dump to the real streams.
				e.PostAction(func() { e.continueLaunchEvals(next) })
			}()
			return
		}
		i++
	}
	if i >= len(e.launchEvals) {
		e.finishLaunchEvals()
	}
	// Otherwise the loop stopped because e.Running went false mid-batch (a
	// script called exit): leave the log for RunArgs' stdio dump.
}

// finishLaunchEvals disposes of the batch's captured output once every script
// has completed.
func (e *Editor) finishLaunchEvals() {
	e.evalCaptureOff()
	if !e.Running {
		// A script instructed mew to exit: the log goes to the real
		// stdout/stderr once serve unwinds (RunArgs).
		return
	}
	text := e.evalCapturedText()
	e.evalCap.mu.Lock()
	e.evalCap.chunks = nil
	e.evalCap.mu.Unlock()
	if strings.TrimSpace(text) != "" {
		// One final fresh buffer holds the accumulated results, focused so
		// they are in view. Seeded as loaded content, the buffer starts
		// unmodified — closing it does not prompt to save.
		buf := e.lib.NewFromString(text)
		e.createMainViewport(buf, nil, true)
	} else if e.evalFocusRestore != "" {
		// Nothing to show: put focus back where the launch left it.
		if w := e.ViewportManager.GetViewport(e.evalFocusRestore); w != nil {
			e.ViewportManager.SetFocus(w.ID)
		}
	}
	e.evalFocusRestore = ""
	e.RequestRender()
}

// dumpEvalOutput writes whatever remains in the capture log to the given
// streams, in occurrence order. After a completed batch the log is empty (the
// results went to a buffer or were blank), so this emits only when a script
// exited the editor — or the session ended — before the batch's results could
// be shown.
func (e *Editor) dumpEvalOutput(stdout, stderr io.Writer) {
	e.evalCap.mu.Lock()
	chunks := e.evalCap.chunks
	e.evalCap.chunks = nil
	e.evalCap.active = false
	e.evalCap.mu.Unlock()
	for _, c := range chunks {
		if c.err {
			io.WriteString(stderr, c.text)
		} else {
			io.WriteString(stdout, c.text)
		}
	}
}
