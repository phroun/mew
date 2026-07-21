package editor

import (
	"fmt"
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
//
// In fully-LOCAL mode (real OS document FS, real ~/.mew tree) a mew: name
// canonicalizes to the REAL file it names: mew:///help/start.txt and
// ~/.mew/help/start.txt are one physical file, so they must be ONE identity
// (one buffer) — and the real-path identity means mew: documents load
// through the full file open path (source tracking, saves, locks, backups)
// instead of as sourceless memory buffers. Under a virtualized mew tree (or
// a virtual document FS) no real path exists, so the mew:/// spelling IS the
// identity.
func (e *Editor) canonicalDocURL(name string) string {
	if name == "" {
		return ""
	}
	if isMewPath(name) {
		if e.osBackedFS() {
			if p, ok := e.mew.LocalPath(name); ok {
				return canonicalOSFileURL(p, e.home)
			}
		}
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
	if e.osBackedFS() {
		return canonicalOSFileURL(name, e.home)
	}
	// A virtual host FS owns its namespace: normalize separators and
	// collapse dot segments as a rooted path, without consulting the OS.
	p := path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(name), "/"))
	return "file://" + p
}

// canonicalOSFileURL canonicalizes a real OS path (tilde-expanded against
// home, made absolute, cleaned, slash-normalized) into file:///... form.
func canonicalOSFileURL(p, home string) string {
	if ex, ok := expandTilde(p, home); ok {
		p = ex
	}
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	p = filepath.ToSlash(filepath.Clean(p))
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

// osBackedFS reports whether documents live on the real OS filesystem —
// either directly (no host FS) or through mew's own OS-backed FileSystem
// (a host wiring OSFileSystem explicitly, as the KittyTK trinket does).
func (e *Editor) osBackedFS() bool {
	if e.usingOSFS {
		return true
	}
	_, ok := e.FS.(osFileSystem)
	return ok
}

// normalizeDocPath canonicalizes a document filename for STORAGE on a
// buffer: mew: names pass through untouched; on an OS-backed document
// filesystem, "~" expands and relative names absolutize, so the buffer
// carries a stable path that survives garland's save/rename dance and any
// working-directory change. Virtual host namespaces are left verbatim.
func (e *Editor) normalizeDocPath(p string) string {
	if p == "" || isMewPath(p) || !e.osBackedFS() {
		return p
	}
	if ex, ok := expandTilde(p, e.home); ok {
		p = ex
	}
	if !filepath.IsAbs(p) {
		if a, err := filepath.Abs(p); err == nil {
			p = a
		}
	}
	return p
}

// loadBufferURL loads the document a canonical URL names: file:/// through
// the full document open path (locks, backups, notices); mew:/// by reading
// mew's support tree. The URL is re-canonicalized first, so in local mode a
// mew:/// name translates to its real file and loads with full source
// tracking; only under a VIRTUALIZED mew tree does the mew:/// branch run,
// producing a memory buffer whose filename is the canonical URL itself (its
// identity round-trips through bufferCanonicalURL).
func (e *Editor) loadBufferURL(url string) (*buffer.Buffer, error) {
	url = e.canonicalDocURL(url)
	prefix, p, ok := urlSplit(url)
	if !ok {
		return nil, fmt.Errorf("not a document URL: %s", url)
	}
	if prefix == "mew://" {
		data, err := e.mew.ReadFile("mew://" + p)
		if err != nil {
			return nil, err
		}
		return e.lib.NewFromBytes(data, url)
	}
	osPath := filepath.FromSlash(p)
	buf, err := e.loadBuffer(osPath)
	if err != nil {
		return nil, err
	}
	buf.SetFilename(osPath)
	return buf, nil
}

// createBufferURL mints a buffer named for a canonical URL that does not
// exist yet, holding the given seed content — the file itself only appears
// when the buffer is first saved. file:/// buffers carry the real path; a
// virtualized mew:/// buffer carries the canonical URL as its filename
// (identity round-trips either way).
func (e *Editor) createBufferURL(url, seed string) (*buffer.Buffer, error) {
	url = e.canonicalDocURL(url)
	prefix, p, ok := urlSplit(url)
	if !ok {
		return nil, fmt.Errorf("not a document URL: %s", url)
	}
	if prefix == "mew://" {
		// []byte(seed) is non-nil even when empty: garland treats a nil
		// DataBytes as "no data source provided" rather than as empty
		// content.
		return e.lib.NewFromBytes([]byte(seed), url)
	}
	buf := e.lib.NewFromString(seed)
	buf.SetFilename(filepath.FromSlash(p))
	return buf, nil
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
