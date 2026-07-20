package editor

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// Canonical document identity.
//
// mew identifies every document by a canonical URL: a scheme, an EMPTY
// authority, and an absolute path from that scheme's root — always spelled
// with "/" separators regardless of platform or input notation:
//
//	file:///home/us/wiki/page.txt    the document filesystem, from its root
//	mew:///syntax/dokuwiki.jsf       mew's own support tree, from its root
//
// Three slashes because the authority is ours (empty = this instance); other
// schemes and real authorities stay open for later. Navigation-facing
// notations — dokuwiki's ":" namespaces, "~" home paths, cwd-relative names —
// are RESOLVED INTO this form; the canonical string itself always uses "/",
// matching the on-disk representation, so two spellings of one file compare
// equal and buffer reuse is exact.

// canonicalDocURL resolves a document name (an OS path, a host-FS name, a
// mew: path, or an already-canonical URL) to its canonical URL. Empty in,
// empty out.
func (e *Editor) canonicalDocURL(name string) string {
	if name == "" {
		return ""
	}
	if isMewPath(name) {
		// confine collapses dot segments and can never escape the mew root,
		// normalizing every accepted spelling (mew:x, mew:/x, mew://x,
		// mew:///x) to the same identity.
		return "mew:///" + confine(name)
	}
	if strings.HasPrefix(name, "file://") {
		// Already URL-form: re-clean the path part so hand-built URLs
		// normalize to the same identity.
		p := strings.TrimPrefix(name, "file://")
		return "file://" + path.Clean("/"+strings.TrimPrefix(p, "/"))
	}
	p := name
	if e.usingOSFS {
		if ex, ok := expandTilde(p, e.home); ok {
			p = ex
		}
		if !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
		p = filepath.ToSlash(filepath.Clean(p))
	} else {
		// A virtual host FS owns its namespace: normalize separators and
		// collapse dot segments as a rooted path, without consulting the OS.
		p = path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(p), "/"))
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// bufferCanonicalURL is the canonical identity of a buffer: the canonical URL
// of its filename, or "" for an unnamed buffer (no identity — never matched
// by reuse lookups).
func (e *Editor) bufferCanonicalURL(b *buffer.Buffer) string {
	if b == nil {
		return ""
	}
	fn := b.GetFilename()
	if fn == "" {
		return ""
	}
	return e.canonicalDocURL(fn)
}

// findOpenBuffer returns an already-open buffer whose canonical identity
// matches url, or nil. "Open" spans every window's active binding AND its nav
// history: a buffer parked one nav_history_prior away is still open, and
// following a link to it must reuse it — two independent buffers on one file
// is exactly the consistency hole the source-safety layer closed.
func (e *Editor) findOpenBuffer(url string) *buffer.Buffer {
	if url == "" {
		return nil
	}
	for _, w := range e.WindowManager.AllWindows() {
		if w.Buffer != nil && e.bufferCanonicalURL(w.Buffer) == url {
			return w.Buffer
		}
		for _, b := range w.StackedBuffers() {
			if e.bufferCanonicalURL(b) == url {
				return b
			}
		}
	}
	return nil
}

// bufferReferencedElsewhere reports whether b is held open — actively or
// stacked in a nav history — by any main-buffer window other than exclude.
// The close path uses it to decide whether dropping a window's history would
// lose a modified buffer's last reference.
func (e *Editor) bufferReferencedElsewhere(b *buffer.Buffer, exclude *window.Window) bool {
	for _, w := range e.getMainBuffers() {
		if w == exclude {
			continue
		}
		if w.Buffer == b {
			return true
		}
		for _, sb := range w.StackedBuffers() {
			if sb == b {
				return true
			}
		}
	}
	return false
}

// openMainBuffers returns every distinct buffer a main-buffer window holds
// open — active bindings plus nav-history stacks. Data-safety paths (close
// liveness, save-all, DEADCAT dumps) enumerate THIS set, so work stacked in a
// window's history is never invisible to them.
func (e *Editor) openMainBuffers() []*buffer.Buffer {
	seen := map[*buffer.Buffer]bool{}
	var out []*buffer.Buffer
	add := func(b *buffer.Buffer) {
		if b != nil && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	for _, w := range e.getMainBuffers() {
		add(w.Buffer)
		for _, b := range w.StackedBuffers() {
			add(b)
		}
	}
	return out
}
