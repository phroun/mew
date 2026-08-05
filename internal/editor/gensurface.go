package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// Generated surfaces: mew's "mew:" scheme documents that are produced on demand
// rather than read from disk (docs/mew-scheme-overlay.md). Each is dynamically
// rendered dokuwiki content, opened IN PLACE in the focused document viewport
// (so back/forward returns to what was there), read-only, and switched into
// link-browse (navigation) mode on open.
//
// Their whole behavior is keyed on the buffer's ADDRESS, not on viewport state:
// bufferGrammar forces the dokuwiki grammar for a mew: buffer, and
// reconcileGrammarOptions applies a fixed option spec (read-only, link-browsing)
// for one — so user config can't pollute the feature and nothing sticks to the
// viewport when the reader navigates away.

// genSurfaces maps a mew: surface name to its content generator. Each generator
// returns dokuwiki source. Add a new surface by registering it here.
var genSurfaces = map[string]func(*Editor) string{
	"buffers":   (*Editor).genBuffersDoc,
	"viewports": (*Editor).genViewportsDoc,
}

// genSurfaceName returns the surface name for a mew: URL ("mew:/buffers" ->
// "buffers"), or "" if url is not a registered generated surface.
func genSurfaceName(url string) string {
	if !isGenPath(url) {
		return ""
	}
	name := strings.TrimLeft(strings.TrimPrefix(url, "mew:"), "/")
	if _, ok := genSurfaces[name]; ok {
		return name
	}
	return ""
}

// openGeneratedSurface renders the named surface and navigates the focused
// document viewport to it, in place. Returns false when the name is unknown or
// there is no document viewport to navigate. Re-opening the surface that is
// already showing refreshes it in place without growing the nav history.
func (e *Editor) openGeneratedSurface(name string) bool {
	gen, ok := genSurfaces[name]
	if !ok {
		return false
	}
	w := e.surfaceTargetViewport()
	if w == nil {
		e.ShowWarning("No document to open the " + name + " list in")
		return false
	}

	url := "mew:/" + name
	buf := e.lib.NewFromString(gen(e))
	buf.SetFilename(url)

	// Refresh in place (no new history) when this surface is already the
	// focused buffer; otherwise navigate there, so back returns to the document.
	if w.Buffer != nil && w.Buffer.GetFilename() == url {
		e.replaceBuffer(w, buf)
	} else {
		e.swapBuffer(w, buf)
	}
	// Fixed generated-surface options: read-only and link-browsing on, then
	// navigation mode. These sit on the viewport but travel with this buffer's
	// nav binding, so returning to the document restores its own options.
	// (bufferGrammar renders it as dokuwiki, keyed on the address; the edit
	// guard also enforces read-only by address, independent of this state.)
	w.ViewState.ReadOnly = true
	w.ViewState.LinkBrowsing = true
	w.BrowseActive = true
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// surfaceTargetViewport is the document viewport a generated surface navigates:
// the focused one when it is a document viewport, else the first content
// document viewport. nil when there is no document viewport at all.
func (e *Editor) surfaceTargetViewport() *viewport.Viewport {
	if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.Type == viewport.DocViewport {
		return w
	}
	for _, w := range e.contentViewports() {
		if w.Type == viewport.DocViewport {
			return w
		}
	}
	return nil
}

// bufferLink renders a dokuwiki link to a buffer by its canonical URL, or plain
// text when the buffer has no followable identity (an unnamed buffer).
func (e *Editor) bufferLink(b *buffer.Buffer, display string) string {
	if b == nil {
		return display
	}
	if url := e.bufferCanonicalURL(b); url != "" {
		return "[[" + url + "|" + display + "]]"
	}
	return display
}

// surfaceBufferLabel is a short, human label for a buffer in a generated list:
// a scheme URL shown whole (it carries its own context), else the base
// filename, else "[unnamed]".
func surfaceBufferLabel(b *buffer.Buffer) string {
	if b == nil {
		return "[unnamed]"
	}
	fn := b.GetFilename()
	if fn == "" {
		return "[unnamed]"
	}
	if isGenPath(fn) || hasScheme(fn) {
		return fn
	}
	return filepath.Base(fn)
}

// openBuffers is the distinct set of buffers currently open across content
// viewports — each viewport's active binding plus its nav-history stack — in a
// stable order (active bindings first).
func (e *Editor) openBuffers() []*buffer.Buffer {
	seen := map[*buffer.Buffer]bool{}
	var out []*buffer.Buffer
	add := func(b *buffer.Buffer) {
		if b == nil || seen[b] {
			return
		}
		seen[b] = true
		out = append(out, b)
	}
	cvs := e.contentViewports()
	for _, w := range cvs {
		add(w.Buffer)
	}
	for _, w := range cvs {
		for _, b := range w.StackedBuffers() {
			add(b)
		}
	}
	return out
}

// genBuffersDoc renders the open-buffer list as dokuwiki. Each named buffer is a
// link to its canonical URL, so following it (in navigation mode) reuses the
// already-open buffer.
func (e *Editor) genBuffersDoc() string {
	focused := e.ViewportManager.GetFocusedViewport()
	var focusedBuf *buffer.Buffer
	if focused != nil {
		focusedBuf = focused.Buffer
	}

	var b strings.Builder
	b.WriteString("====== Open Buffers ======\n\n")
	bufs := e.openBuffers()
	if len(bufs) == 0 {
		b.WriteString("//No open buffers.//\n")
		return b.String()
	}
	for _, buf := range bufs {
		marks := ""
		if buf.IsModified() {
			marks += " **[modified]**"
		}
		if buf == focusedBuf {
			marks += " //(current)//"
		}
		fmt.Fprintf(&b, "  - %s%s\n", e.bufferLink(buf, surfaceBufferLabel(buf)), marks)
	}
	return b.String()
}

// genViewportsDoc renders the open-viewport list as dokuwiki: each viewport's
// id, kind, and dock, with a link to the buffer it holds.
func (e *Editor) genViewportsDoc() string {
	focused := e.ViewportManager.GetFocusedViewport()

	var b strings.Builder
	b.WriteString("====== Open Viewports ======\n\n")
	var rows int
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Type == viewport.PromptViewport {
			continue
		}
		rows++
		current := ""
		if w == focused {
			current = " //(focused)//"
		}
		bufPart := "[no buffer]"
		if w.Buffer != nil {
			bufPart = e.bufferLink(w.Buffer, surfaceBufferLabel(w.Buffer))
		}
		fmt.Fprintf(&b, "  - %s — %s / %s%s\n",
			bufPart, viewportTypeName(w.Type), viewportDockName(w.Dock), current)
	}
	if rows == 0 {
		b.WriteString("//No open viewports.//\n")
	}
	return b.String()
}

// viewportTypeName / viewportDockName give short human labels for a viewport's
// kind and dock position.
func viewportTypeName(t viewport.ViewportType) string {
	switch t {
	case viewport.DocViewport:
		return "document"
	case viewport.ToolViewport:
		return "tool"
	case viewport.PromptViewport:
		return "prompt"
	default:
		return "other"
	}
}

func viewportDockName(d viewport.DockPosition) string {
	switch d {
	case viewport.DockNone:
		return "main"
	case viewport.DockTop:
		return "top"
	case viewport.DockBottom:
		return "bottom"
	default:
		return "?"
	}
}
