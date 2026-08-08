package editor

import (
	"fmt"
	"strings"
	"sync"

	"github.com/phroun/mew/internal/buffer"
)

// filter.go runs a command as a FILTER — JOE's "filter block through command":
// the child runs headless on pipes (not a terminal, not purfecterm), so mew can
// feed its stdin and read its stdout/stderr itself and route each stream where
// the user asked (--inblock/--outblock, --stdin/--stdout/--stderr). See
// execargs.go for the routing surface and pty.go for the pipe session seam.
//
// The document stays live throughout and every continuous operation is driven by
// EPHEMERAL cursors garland rides across concurrent edits, so the user may keep
// typing — and may even repurpose the block markers — without derailing the run.
// There is no explicit undo transaction: the streamed inserts ride garland's
// natural coalescing.

// runFilter is the filter path of exec/shell, taken when a stream is routed
// somewhere other than the terminal (execSpec.filtering()). It gates, spawns the
// child on pipes through the ONE PTYProvider, and hands the session to the
// streaming machinery. Reports whether the request was launched.
func (e *Editor) runFilter(spec execSpec) bool {
	if e.Config.PTYProvider == nil {
		e.ShowWarning("No terminal provider: this host does not grant sessions")
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}
	if why := wikiDeclineExec(w.WikiName); why != "" {
		e.ShowWarning(degenerateToASCII(why))
		return false
	}
	if err := spec.resolveRoutes(); err != nil {
		e.ShowWarning("exec: " + err.Error())
		return false
	}

	needBlock := spec.Stdin == routeBlock || spec.Stdout == routeBlock || spec.Stderr == routeBlock
	replacesBlock := spec.Stdout == routeBlock || spec.Stderr == routeBlock
	if replacesBlock && e.contentLocked() {
		e.ShowWarning("exec: this buffer is read-only")
		return false
	}

	fr := &filterRun{
		e:            e,
		buf:          w.Buffer,
		spec:         spec,
		snapshotDone: make(chan struct{}),
		docs:         map[streamRoute]*filterDoc{},
	}
	if needBlock {
		sl, sr, el, er, ok := w.Buffer.GetBlockRange()
		if !ok {
			e.ShowWarning("exec: no block marked")
			return false
		}
		// Anchor the block with edit-riding ephemeral cursors, not fixed
		// coordinates: garland carries them across any concurrent edit, so a
		// snapshot taken now and a replace done later both act on the right region
		// even if the user is typing meanwhile.
		fr.blockStart = w.Buffer.NewEphemeralCursor()
		fr.blockStart.SeekLineRune(sl, sr)
		fr.blockEnd = w.Buffer.NewEphemeralCursor()
		fr.blockEnd.SeekLineRune(el, er)
	}

	req := PTYRequest{
		CWD:    e.bufferCWD(w.Buffer),
		Args:   spec.Args,
		Shell:  spec.Shell,
		Method: strings.TrimSpace(spec.Method),
		Stdin:  StreamPipe,
		Stdout: StreamPipe,
		Stderr: StreamPipe,
	}
	if !spec.Shell {
		req.Command = strings.TrimSpace(spec.Program)
	}
	sess, err := e.Config.PTYProvider(req)
	if err != nil || sess == nil {
		fr.releaseAnchors()
		msg := "Host refused the session"
		if err != nil {
			msg = "Cannot execute: " + err.Error()
		}
		e.ShowWarning(msg)
		return false
	}

	fr.start(sess)

	name := spec.Program
	if spec.Shell {
		name = "the shell"
	}
	e.ShowNotification("Filtering through " + name)
	return true
}

// filterRun holds one filter's shared state across its goroutines.
type filterRun struct {
	e    *Editor
	buf  *buffer.Buffer // the source/target buffer (the block lives here)
	spec execSpec

	// blockStart/blockEnd anchor the marked block as edit-riding cursors (nil
	// when no stream is routed to the block). snapshotDone is closed once the
	// inblock snapshot has been copied out, so a block-replacing output waits for
	// it before deleting the block.
	blockStart, blockEnd *buffer.Cursor
	snapshotDone         chan struct{}

	// The in-place block replace (--outblock) state, touched by the single output
	// goroutine routed to the block.
	blockMu      sync.Mutex
	blockStarted bool
	blockFirst   bool
	blockWrite   *buffer.Cursor

	// docs are the lazily-created new-buffer sinks, keyed by route so stdout and
	// stderr aimed at the same sink (both outbuffer, say) merge into one document.
	docMu sync.Mutex
	docs  map[streamRoute]*filterDoc
}

// filterDoc is one new document buffer a stream is streamed into.
type filterDoc struct {
	mu     sync.Mutex
	buf    *buffer.Buffer
	cursor *buffer.Cursor
}

// start launches the stdin feeder and the output readers, then a watcher that
// reaps the child and reports its exit once both outputs reach EOF.
func (fr *filterRun) start(sess PTYSession) {
	go fr.pumpStdin(sess)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fr.pumpOutput(sess.Read, fr.spec.Stdout)
	}()
	// stderr is drained on its own pipe whenever the session offers one, even when
	// routed to null — an undrained stderr pipe would eventually block the child.
	if sr, ok := sess.(PTYStderr); ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fr.pumpOutput(sr.ReadStderr, fr.spec.Stderr)
		}()
	}

	go func() {
		wg.Wait()
		code, exited := 0, false
		if es, ok := sess.(PTYExitStatus); ok {
			code, exited = es.ExitStatus()
		}
		_ = sess.Close()
		fr.finish(code, exited)
	}()
}

// pumpStdin sources the child's stdin per the route and half-closes it at EOF so
// a filter reading to completion finishes. For --inblock it snapshots the block
// into a private temp buffer first (so the block may then be replaced in place),
// then streams that out, freeing the consumed head and pruning undo as it goes.
func (fr *filterRun) pumpStdin(sess PTYSession) {
	defer func() {
		if c, ok := sess.(PTYStdinCloser); ok {
			_ = c.CloseStdin()
		}
	}()

	if fr.spec.Stdin != routeBlock {
		close(fr.snapshotDone) // nothing to snapshot; a block output may proceed
		return                 // null stdin: the deferred half-close signals EOF
	}

	temp := fr.e.lib.New()
	defer temp.Close()
	fr.snapshotBlock(temp)
	close(fr.snapshotDone) // the block may now be deleted/replaced by an output

	fed := 0
	for {
		chunk, ok := temp.ReadDropFront(32 * 1024)
		if !ok {
			break
		}
		if _, err := sess.Write(chunk); err != nil {
			break
		}
		if fed++; fed%64 == 0 {
			temp.PruneUndo()
		}
	}
}

// snapshotBlock copies the marked block into temp, streaming it chunk by chunk
// through a range reader so a huge block is never materialized whole.
func (fr *filterRun) snapshotBlock(temp *buffer.Buffer) {
	sl, sr := fr.blockStart.GetPosition()
	el, er := fr.blockEnd.GetPosition()
	rr := fr.buf.NewRangeReader(sl, sr, el, er)
	defer rr.Release()
	dst := temp.NewEphemeralCursor()
	defer dst.Release()
	dst.SeekLineRune(0, 0)
	buf := make([]byte, 32*1024)
	for {
		n, err := rr.Read(buf)
		if n > 0 {
			dst.InsertString(string(buf[:n]), nil, false)
		}
		if err != nil {
			break
		}
	}
}

// pumpOutput reads one output stream to EOF and routes each chunk.
func (fr *filterRun) pumpOutput(read func([]byte) (int, error), route streamRoute) {
	buf := make([]byte, 32*1024)
	for {
		n, err := read(buf)
		if n > 0 {
			fr.route(route, append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			return
		}
	}
}

// route sends one output chunk to its destination.
func (fr *filterRun) route(r streamRoute, chunk []byte) {
	switch r {
	case routeNull:
		// discarded (still drained above so the child never blocks)
	case routeBlock:
		fr.writeBlock(chunk)
	case routeOutBuffer, routeErrBuffer:
		fr.writeDoc(r, chunk)
	}
}

// writeBlock streams an output stream in place of the marked block. On the first
// chunk it waits for the inblock snapshot (if any) to be safely copied out, then
// deletes the old block content once; every chunk is inserted with
// insertBefore=true so the _block_end mark parked at the tail rides forward on
// its own, and _block_begin is re-pinned to the start after the first insert.
// Only one output stream is ever routed here (guarded in resolveRoutes).
func (fr *filterRun) writeBlock(chunk []byte) {
	fr.blockMu.Lock()
	defer fr.blockMu.Unlock()
	if !fr.blockStarted {
		<-fr.snapshotDone
		sl, sr := fr.blockStart.GetPosition()
		el, er := fr.blockEnd.GetPosition()
		fr.buf.DeleteTextRange(sl, sr, el, er)
		fr.blockWrite = fr.buf.NewEphemeralCursor()
		fr.blockWrite.SeekLineRune(sl, sr)
		fr.blockStarted = true
		fr.blockFirst = true
	}
	fr.blockWrite.InsertStringBefore(string(chunk))
	if fr.blockFirst {
		sl, sr := fr.blockStart.GetPosition()
		fr.buf.SetMark("_block_begin", sl, sr)
		fr.blockFirst = false
	}
	fr.e.RequestRender()
}

// writeDoc streams an output stream into a new document buffer, created lazily on
// the first byte and surfaced in a viewport. Two streams routed to the same sink
// share one document.
func (fr *filterRun) writeDoc(r streamRoute, chunk []byte) {
	fr.docMu.Lock()
	d := fr.docs[r]
	if d == nil {
		d = &filterDoc{buf: fr.e.lib.New()}
		d.buf.SetFilename(docTitle(r))
		d.cursor = d.buf.NewEphemeralCursor()
		d.cursor.SeekLineRune(0, 0)
		fr.docs[r] = d
		b := d.buf
		// Viewport creation is main-loop work; the content is written off it.
		fr.e.PostAction(func() { fr.e.createMainViewport(b, nil, false) })
	}
	fr.docMu.Unlock()

	d.mu.Lock()
	d.cursor.InsertString(string(chunk), nil, false)
	d.mu.Unlock()
	fr.e.RequestRender()
}

// docTitle names a new-buffer sink so the user can tell stdout from stderr.
func docTitle(r streamRoute) string {
	if r == routeErrBuffer {
		return "filter-errors"
	}
	return "filter-output"
}

// finish reports the outcome and releases the run's anchors. The block is
// replaced regardless of exit code (the whole filter is undoable), and a
// non-zero status is surfaced in the notification.
func (fr *filterRun) finish(code int, exited bool) {
	fr.releaseAnchors()
	if fr.blockWrite != nil {
		fr.blockWrite.Release()
		fr.blockWrite = nil
	}
	msg := "Filter complete"
	if exited && code != 0 {
		msg = fmt.Sprintf("Filter command exited with status %d", code)
	}
	fr.e.ShowNotification(msg)
	fr.e.RequestRender()
}

// releaseAnchors drops the block-anchor cursors. Safe to call more than once.
func (fr *filterRun) releaseAnchors() {
	if fr.blockStart != nil {
		fr.blockStart.Release()
		fr.blockStart = nil
	}
	if fr.blockEnd != nil {
		fr.blockEnd.Release()
		fr.blockEnd = nil
	}
}
