package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
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
// Their behavior is keyed on the buffer's ADDRESS, not on viewport state:
// bufferGrammar forces the dokuwiki grammar for a mew: buffer, and the edit
// guard enforces read-only for one — so user config can't pollute the feature
// and nothing sticks to the viewport when the reader navigates away.
//
// Each list entry is a dokuwiki link "[[<target>|<title>]]" whose TARGET is a
// STABLE handle for the item — a buffer handle (buffer.Handle), or a viewport
// id. Following one does NOT go through ordinary wiki/link resolution: the
// surface's follow handler resolves the target back to the exact buffer or
// viewport and turns it into an operation. The follow handler is the one place
// that decides what following an entry DOES.

// genSurface is a registered generated surface: how to render it, and what
// following one of its entries (by stable target) means.
type genSurface struct {
	render func(*Editor) string
	follow func(e *Editor, target string) bool
}

// genSurfaces is the registry. Add a surface by registering a render/follow
// pair here.
var genSurfaces = map[string]genSurface{
	"buffers":   {render: (*Editor).renderBuffers, follow: (*Editor).followBuffer},
	"viewports": {render: (*Editor).renderViewports, follow: (*Editor).followViewport},
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
// document viewport to it, in place. Re-opening the surface already shown
// refreshes it without growing the nav history.
func (e *Editor) openGeneratedSurface(name string) bool {
	s, ok := genSurfaces[name]
	if !ok {
		return false
	}
	w := e.surfaceTargetViewport()
	if w == nil {
		e.ShowWarning("No document to open the " + name + " list in")
		return false
	}

	url := "mew:/" + name
	buf := e.lib.NewFromString(s.render(e))
	buf.SetFilename(url)

	// Refresh in place (no new history) when this surface is already the focused
	// buffer; otherwise navigate there, so back returns to the document.
	if w.Buffer != nil && w.Buffer.GetFilename() == url {
		e.replaceBuffer(w, buf)
	} else {
		e.swapBuffer(w, buf)
	}
	// Fixed generated-surface options: read-only and link-browsing on, then
	// navigation mode. These travel with this buffer's nav binding, so returning
	// to the document restores its own options with no leak.
	w.ViewState.ReadOnly = true
	w.ViewState.LinkBrowsing = true
	w.BrowseActive = true
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// followGeneratedSurfaceLink dispatches a followed link inside a generated
// surface to that surface's follow handler. Reports (handled, isSurface):
// isSurface is false when w's buffer is not a generated surface, so the caller
// falls through to ordinary link resolution.
func (e *Editor) followGeneratedSurfaceLink(w *viewport.Viewport, target string) (handled, isSurface bool) {
	if w == nil || w.Buffer == nil {
		return false, false
	}
	name := genSurfaceName(w.Buffer.GetFilename())
	if name == "" {
		return false, false
	}
	return genSurfaces[name].follow(e, target), true
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

// linkItem writes one dokuwiki list entry: a link with the given target and
// title, plus any trailing marks after the link.
func linkItem(b *strings.Builder, target, title, marks string) {
	fmt.Fprintf(b, "  - [[%s|%s]]%s\n", target, linkTitle(title), marks)
}

// linkTitle keeps a display string safe as a dokuwiki link title: a "]]" would
// close the link early, so soften any bracket.
func linkTitle(s string) string {
	if strings.ContainsAny(s, "]|") {
		s = strings.NewReplacer("]", ")", "|", "/").Replace(s)
	}
	return s
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

// bufferByHandle finds the open buffer with the given handle, or nil.
func (e *Editor) bufferByHandle(h uint64) *buffer.Buffer {
	if h == 0 {
		return nil
	}
	for _, b := range e.openBuffers() {
		if b.Handle() == h {
			return b
		}
	}
	return nil
}

// renderBuffers renders the open-buffer list as dokuwiki; each entry links to
// its buffer by handle.
func (e *Editor) renderBuffers() string {
	focused := e.ViewportManager.GetFocusedViewport()
	var focusedBuf *buffer.Buffer
	if focused != nil {
		focusedBuf = focused.Buffer
	}

	bufs := e.openBuffers()
	var b strings.Builder
	b.WriteString("====== Open Buffers ======\n\n")
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
		linkItem(&b, strconv.FormatUint(buf.Handle(), 10), surfaceBufferLabel(buf), marks)
	}
	return b.String()
}

// followBuffer navigates the focused document viewport to the buffer the entry
// names — the same in-place navigation opening the surface itself performs, so
// back returns to the list.
func (e *Editor) followBuffer(target string) bool {
	h, err := strconv.ParseUint(strings.TrimSpace(target), 10, 64)
	if err != nil {
		return false
	}
	buf := e.bufferByHandle(h)
	if buf == nil {
		e.ShowNotification("That buffer is no longer open")
		return true
	}
	w := e.surfaceTargetViewport()
	if w == nil {
		return false
	}
	e.swapBuffer(w, buf)
	e.ensureCursorVisible(w)
	return true
}

// renderViewports renders the open-viewport list as dokuwiki; each entry links
// to its viewport by id.
func (e *Editor) renderViewports() string {
	focused := e.ViewportManager.GetFocusedViewport()

	var b strings.Builder
	b.WriteString("====== Open Viewports ======\n\n")
	var rows int
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Type == viewport.PromptViewport {
			continue
		}
		rows++
		label := surfaceBufferLabel(w.Buffer)
		if w.Buffer == nil {
			label = "[no buffer]"
		}
		marks := fmt.Sprintf(" — %s / %s", viewportTypeName(w.Type), viewportDockName(w.Dock))
		if w == focused {
			marks += " //(focused)//"
		}
		linkItem(&b, w.ID, label, marks)
	}
	if rows == 0 {
		b.WriteString("//No open viewports.//\n")
	}
	return b.String()
}

// followViewport switches which viewport the surface's tile shows: it closes
// the surface (falling back to the document it replaced and dropping itself
// from the nav chain), then repoints the tile to the chosen viewport.
func (e *Editor) followViewport(target string) bool {
	target = strings.TrimSpace(target)
	dest := e.ViewportManager.GetViewport(target)
	if dest == nil {
		e.ShowNotification("That viewport is no longer open")
		return true
	}
	e.switchTileViewport(dest)
	return true
}

// switchTileViewport carries out the viewports-surface operation: close the
// surface (fall back to the document it replaced) and repoint the tile that was
// showing the surface so it now shows dest instead.
//
// Tiles-to-viewports is many-to-many by design: dest may already be shown in
// other tiles (it stays shown there too), and the reverted surface viewport
// simply becomes an untiled background viewport that a later viewports listing
// can pull into any tile. With no active tiler (headless / tests) this reduces
// to a focus switch.
func (e *Editor) switchTileViewport(dest *viewport.Viewport) {
	sw := e.surfaceTargetViewport() // the viewport currently showing the surface
	if dest == nil {
		return
	}
	// Close the surface: revert its viewport to the document it navigated from.
	if sw != nil && sw != dest {
		sw.NavHistoryPrior()
	}
	// Repoint the surface's tile to dest, so the tile shows dest now.
	if e.tiler != nil && sw != nil && sw.ID != dest.ID {
		for _, h := range e.tiler.Get(sw.ID, false) {
			e.tiler.Set(h, dest.ID)
		}
	}
	e.ViewportManager.SetFocus(dest.ID)
	e.RequestRender()
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
