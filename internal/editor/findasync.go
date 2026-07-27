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
// ^C reaches this through the cancel command: with no prompt open, a running
// find is what a cancel means, so it stops the thread instead of falling
// through to buffer_close and its "LOSE CHANGES" question (see cancelFind).

// findToastTag tags the background find's progress toast, so each pass replaces
// the previous one and the toast can be cleared by tag.
const findToastTag = "find_progress"

// findToastMessage is the progress toast; its TFC code resolves to whatever key
// is bound to cancel, which is exactly the key that stops the search.
const findToastMessage = "Searching (%keys#cancel.buffer_close% to cancel)..."

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
func (e *Editor) armSearchToast(message, tag string, seq int, current func() int, stop, settled *atomic.Bool) {
	if stop.Load() || settled.Load() || current() != seq {
		return
	}
	e.showTransient(message, "notification", tag, true)
	e.RequestRender()
}

// afterSearchGrace runs the toast payload once the grace period has passed,
// hopping back onto the main loop to do it (a toast creates a viewport). The
// returned timer is stopped by the pass when it finishes; should it have fired
// already, the payload's own checks suppress the toast.
func (e *Editor) afterSearchGrace(fn func()) *time.Timer {
	return time.AfterFunc(searchToastGrace, func() { e.PostAction(fn) })
}

// armFindToast is the find's grace-timer payload.
func (e *Editor) armFindToast(seq int, stop, settled *atomic.Bool) {
	e.armSearchToast(findToastMessage, findToastTag, seq,
		func() int {
			if e.findRun == nil {
				return -1
			}
			return e.findRun.seq
		}, stop, settled)
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
	seq     int
	stop    *atomic.Bool
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

// running reports whether a pass has been started and not yet stopped — what
// the cancel command consults to decide that ^C means "stop the search".
func (fr *findRun) running() bool {
	return fr != nil && fr.stop != nil && !fr.stop.Load()
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

	term := state.Term
	backwards := opts.backwards

	// The toast waits out the grace period: a search that lands inside it never
	// raises one. settled marks this pass finished, so a timer that fires in the
	// gap between the scan ending and its result being applied stays quiet.
	settled := &atomic.Bool{}
	grace := e.afterSearchGrace(func() { e.armFindToast(seq, stop, settled) })

	fr.done.Add(1)
	go func() {
		defer fr.done.Done()
		defer func() {
			settled.Store(true)
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
		e.clearTaggedTransient(findToastTag)
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
			e.clearTaggedTransient(findToastTag)
		}
		e.RequestRender()
		return
	}

	// This IS the newest pass, and it finished: the search is caught up.
	e.clearTaggedTransient(findToastTag)

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

// cancelFind stops a running background find and takes its progress toast down,
// reporting whether there was one to stop. It is what makes ^C mean "call off
// the search" while one is running: the cancel command consults it before
// reporting failure, so the ^C chain never falls through to buffer_close (and
// its "LOSE CHANGES" question) just because a long search is underway.
func (e *Editor) cancelFind() bool {
	if !e.findRun.running() {
		return false
	}
	e.findRun.stopPass()
	e.clearTaggedTransient(findToastTag)
	e.ShowNotification("Search cancelled")
	e.RequestRender()
	return true
}
