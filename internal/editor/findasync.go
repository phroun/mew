package editor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// Background find. find and find_next hand their scan to a goroutine instead of
// walking the document on the main loop, so a search over a large file (or, with
// the "a" option, every open buffer) leaves the editor responsive and can be
// called off. The mechanics mirror the incremental search's (see isearch.go):
// one pass at a time, superseded by the next, with a progress toast up exactly
// while a pass is outstanding.
//
// Everything the goroutine reads is prepared for it on the main loop: a compiled
// matcher and a findSource per buffer, each carrying a garland cursor of its own
// (buffer.LineReader), never the shared read cursor the renderer seeks. Nothing
// but the scan happens off-thread — the caret move, the wrap announcement and
// the not-found notice all run back on the main loop in findPump.
//
// ^C reaches this through the cancel command, but ONLY while the progress toast
// is up. A find has no prompt and no visible mode once its first search has
// begun — ^L simply goes to the next match — so there is nothing for a cancel to
// mean. The one exception is the search that
// outran its grace period and put a message on screen saying which key stops it:
// that message is a promise, and it is the whole reason cancel looks here at all
// (see cancelFind).

// findToastTag tags the background find's progress toast, so each pass replaces
// the previous one and the toast can be cleared by tag.
const findToastTag = "find_progress"

// findToastMessage is the progress toast; its TFC code resolves to whatever key
// is bound to cancel, which is exactly the key that stops the search.
const findToastMessage = "Searching (%keys#cancel.viewport_close% to cancel)..."

// searchToastGrace is how long a background scan may run before its progress
// toast appears — shared by the find and the incremental search. Nearly every
// search finishes well inside it and never raises a toast at all, so typing in
// the incremental search does not strobe one per keystroke; only a search
// actually worth waiting for announces itself and offers the cancel key.
const searchToastGrace = 200 * time.Millisecond

// armSearchToast raises a background scan's progress toast, unless the pass it
// belongs to is already over: stopped (superseded or cancelled), finished, or
// no longer the current one. It is the grace timer's payload — and, since it
// takes no timing of its own, what a test drives directly to stand in for the
// timer having fired.
//
// It reports whether the toast actually went up, because that is the fact the
// find's cancel depends on: the message names the cancel key, and only a search
// that made that promise may be called off by it.
func (e *Editor) armSearchToast(message, tag string, seq int, current func() int, stop, settled *atomic.Bool) bool {
	if stop.Load() || settled.Load() || current() != seq {
		return false
	}
	e.showTransient(message, "notification", tag, true)
	e.RequestRender()
	return true
}

// afterSearchGrace runs the toast payload once the grace period has passed,
// hopping back onto the main loop to do it (a toast creates a viewport). The
// returned timer is stopped by the pass when it finishes; should it have fired
// already, the payload's own checks suppress the toast.
func (e *Editor) afterSearchGrace(fn func()) *time.Timer {
	return time.AfterFunc(searchToastGrace, func() { e.PostAction(fn) })
}

// armFindToast is the find's grace-timer payload. Raising the toast is what
// makes the pass cancelable: until then the search is invisible and ^C has
// nothing to be about.
func (e *Editor) armFindToast(seq int, stop, settled *atomic.Bool) {
	up := e.armSearchToast(findToastMessage, findToastTag, seq,
		func() int {
			if e.findRun == nil {
				return -1
			}
			return e.findRun.seq
		}, stop, settled)
	if up && e.findRun != nil {
		e.findRun.toasted = true
	}
}

// findRunResult is one completed (or abandoned) background find pass.
type findRunResult struct {
	seq        int
	term       string
	cancelled  bool
	found      bool
	viewportID string
	line       int
	col        int
	wrapped    bool
	backwards  bool
}

// findRun is the state of the background find: the newest pass's generation and
// stop flag, the goroutines still winding down, and the finished passes waiting
// for findPump to apply them on the main loop.
type findRun struct {
	seq  int
	stop *atomic.Bool
	// settled is the newest pass's own "the scan is over" flag, kept here so
	// the main loop can tell a search still grinding from one that finished.
	// Without it a pass that ended normally looks eternally in flight — stop is
	// only ever raised to ABANDON one — and every later ^C would be swallowed by
	// a search that ended long ago.
	settled *atomic.Bool
	// toasted records that this pass outran its grace period and put the
	// progress message on screen. That message names the cancel key, and it is
	// the only thing that makes a find cancelable at all: see cancelFind.
	toasted bool
	done    sync.WaitGroup
	mu      sync.Mutex
	pending []findRunResult
}

// stopPass abandons the in-flight pass, if any. It does not wait: the goroutine
// notices the flag and its answer is discarded as stale.
func (fr *findRun) stopPass() {
	if fr != nil && fr.stop != nil {
		fr.stop.Store(true)
	}
}

// running reports whether a pass is still scanning: started, not abandoned, and
// not yet finished. findPump consults it to decide whether the toast still has
// something to wait for.
func (fr *findRun) running() bool {
	if fr == nil || fr.stop == nil || fr.stop.Load() {
		return false
	}
	return fr.settled == nil || !fr.settled.Load()
}

// cancelable reports whether ^C should mean "call off the search". A find is
// cancelable only while it is BOTH still scanning and showing the message that
// told the user which key stops it — everything else about a find is
// promptless and modeless, so a cancel there would have nothing to act on and
// would only steal the key from viewport_close.
func (fr *findRun) cancelable() bool {
	return fr != nil && fr.toasted && fr.running()
}

// takePending removes and returns every finished pass collected so far.
func (fr *findRun) takePending() []findRunResult {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	out := fr.pending
	fr.pending = nil
	return out
}

// findStepAsync is findStep off the main loop: it prepares the scan, starts it
// on a goroutine and returns immediately. The caret lands (or "Not found"
// appears) when findPump applies the result. It reports whether a pass was
// actually started — a bad option string or a missing target reports false, with
// the reason already on screen, so the calling command can fail honestly.
//
// count and allowWrap carry findStep's meaning exactly: the count'th match in
// the scan direction, wrapping only for a single-occurrence step when the
// searchWrap option permits.
func (e *Editor) findStepAsync(state viewport.FindState, count int, allowWrap bool) bool {
	opts, err := parseFindOptions(state.Options)
	if err != nil {
		e.ShowWarning(err.Error())
		return false
	}
	w := e.resolveTargetMain()
	if w == nil || w.Buffer == nil {
		return false
	}
	opts.ignoreCase = opts.ignoreCase || e.optBool(w, "searchignorecase", e.Config.SearchIgnoreCase)
	m, err := buildMatcher(state.Term, opts, e.optBool(w, "searchregex", e.Config.SearchRegex))
	if err != nil {
		e.ShowWarning(err.Error())
		return false
	}
	if count < 1 {
		count = 1
	}

	// The ring of buffers to scan, each behind a reader of its own. Built here,
	// on the main loop, because it walks the viewport list.
	mains, _, start := e.findViewportSources(opts, w)
	if len(mains) == 0 {
		return false
	}
	srcs := make([]findSource, len(mains))
	readers := make([]*buffer.LineReader, 0, len(mains))
	for i, mw := range mains {
		srcs[i] = findSource{id: mw.ID}
		if mw.Buffer != nil {
			r := mw.Buffer.NewLineReader()
			readers = append(readers, r)
			srcs[i].src = r
		}
	}

	// The scan starts just past the caret, exactly as findStep does.
	line, col := w.CursorPos().Line, w.CursorPos().Rune+1
	if opts.backwards {
		col = w.CursorPos().Rune - 1
		if col < 0 {
			line, col = line-1, farCol
		}
	}
	searchWrap := e.optBool(w, "searchwrap", e.Config.SearchWrap)

	if e.findRun == nil {
		e.findRun = &findRun{}
	}
	fr := e.findRun
	fr.stopPass() // supersede whatever was running
	fr.seq++
	seq := fr.seq
	stop := &atomic.Bool{}
	fr.stop = stop
	// A fresh pass has made no promise yet: whatever the last one showed, this
	// one is silent until its own grace period runs out.
	fr.toasted = false

	term := state.Term
	backwards := opts.backwards

	// The toast waits out the grace period: a search that lands inside it never
	// raises one. settled marks this pass finished, so a timer that fires in the
	// gap between the scan ending and its result being applied stays quiet — and
	// so the main loop can tell a live search from a finished one.
	settled := &atomic.Bool{}
	fr.settled = settled
	grace := e.afterSearchGrace(func() { e.armFindToast(seq, stop, settled) })

	fr.done.Add(1)
	go func() {
		defer fr.done.Done()
		defer func() {
			grace.Stop()
			for _, r := range readers {
				r.Release()
			}
		}()

		res := findRunResult{seq: seq, term: term, backwards: backwards}
		cur := start
		var last findHit
		var ok bool
		for k := 0; k < count; k++ {
			// Only a single-occurrence step may wrap; counting never does.
			wrapThis := allowWrap && searchWrap && count == 1
			last, ok = findInSources(m, opts, srcs, cur, line, col, wrapThis, stop.Load)
			if !ok {
				break
			}
			cur = last.idx
			if backwards {
				line, col = last.line, last.col-1
				if col < 0 {
					line, col = line-1, farCol
				}
			} else {
				adv := last.length
				if adv < 1 {
					adv = 1
				}
				line, col = last.line, last.col+adv
			}
		}
		if ok {
			res.found = true
			res.viewportID = srcs[last.idx].id
			res.line, res.col, res.wrapped = last.line, last.col, last.wrapped
		}
		res.cancelled = stop.Load()

		// Marked BEFORE the result is handed over, not in a defer: findPump runs
		// on the main loop and may get there first, and a pass whose scan is over
		// must not still read as in flight when it does.
		settled.Store(true)

		fr.mu.Lock()
		fr.pending = append(fr.pending, res)
		fr.mu.Unlock()

		// Hand the main loop the job of applying it. With no action port (the
		// session is winding down, or a headless test) the result simply waits
		// in pending for whoever pumps next.
		e.PostAction(e.findPump)
	}()
	return true
}

// findPump applies finished background find passes on the main loop: the newest
// pass wins, stale and cancelled ones are dropped, and the toast clears once
// nothing is left to wait for.
func (e *Editor) findPump() {
	fr := e.findRun
	if fr == nil {
		e.dropFindToast()
		return
	}

	var latest *findRunResult
	for _, res := range fr.takePending() {
		if res.cancelled || res.seq != fr.seq {
			continue // superseded or abandoned: its answer is not wanted
		}
		r := res
		latest = &r
	}
	if latest == nil {
		// Everything collected was stale. A newer pass still running keeps the
		// toast; with nothing outstanding it comes down, so a cancelled pass
		// whose answer arrives late cannot leave it stranded on screen.
		if !fr.running() {
			e.dropFindToast()
		}
		e.RequestRender()
		return
	}

	// This IS the newest pass, and it finished: the search is caught up.
	e.dropFindToast()

	if !latest.found {
		e.ShowNotification("Not found: " + latest.term)
		e.RequestRender()
		return
	}
	tw := e.ViewportManager.GetViewport(latest.viewportID)
	if tw == nil {
		e.RequestRender()
		return
	}
	e.moveToMatch(tw, latest.line, latest.col)
	e.announceFindWrap(matchInfo{w: tw, wrapped: latest.wrapped}, latest.backwards)
	e.RequestRender()
}

// dropFindToast takes the progress message down and, with it, the promise it
// carried: the message named the cancel key, so once it is gone ^C has nothing
// to honour and belongs to whatever else it is bound to. The two are always
// changed together, which is why nothing clears the transient by hand.
func (e *Editor) dropFindToast() {
	if e.findRun != nil {
		e.findRun.toasted = false
	}
	e.clearTaggedTransient(findToastTag)
}

// cancelFind stops a background find that is ASKING to be stopped, reporting
// whether there was one. The cancel command consults it before reporting
// failure, so a long search underway does not lose the ^C chain to viewport_close
// and its "LOSE CHANGES" question.
//
// The condition is narrow on purpose. A find is not a mode: it has no prompt
// once the first search has begun and ^L simply goes to the next match, so there
// is no ongoing find to abandon — ^C there should fall through to whatever else
// it is bound to, exactly as it did before background find existed.
// The single exception is the search slow enough to have put a message on screen
// naming the cancel key. That message is a promise to the user, and honouring it
// is the entire reason this function exists.
func (e *Editor) cancelFind() bool {
	if !e.findRun.cancelable() {
		return false
	}
	e.findRun.stopPass()
	e.dropFindToast()
	e.ShowNotification("Search cancelled")
	e.RequestRender()
	return true
}
