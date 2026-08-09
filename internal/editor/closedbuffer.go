package editor

import (
	"fmt"
	"strings"

	"github.com/phroun/mew/internal/buffer"
)

// The closed-buffer tombstone. When buffer_close closes a buffer everywhere it
// is referenced, its nav-history slots are not purged — they are REPLACED with a
// read-only placeholder buffer that says the buffer was closed and, when it had
// a real file on disk, offers a Re-open link back to it. The placeholder carries
// the mew:/closed address, so it renders in the dokuwiki grammar and is read-only
// by address like the other generated surfaces; its Re-open link routes through
// the "closed" surface's follow handler below.

// closedPlaceholderURL is the address every tombstone carries.
const closedPlaceholderURL = "mew:/closed"

// reopenableName reports whether a closed buffer's name can be re-opened: it
// must have a name, and not be one of mew's own generated (mew:) surfaces —
// there is nothing on disk behind those to reload.
func reopenableName(name string) bool {
	return name != "" && !isGenPath(name)
}

// newClosedPlaceholder builds the read-only tombstone shown wherever a
// globally-closed buffer used to be referenced. It names the closed buffer and,
// when that buffer had a real file, offers a Re-open link whose target is the
// filename (followClosedReopen loads it back in place).
func (e *Editor) newClosedPlaceholder(name string) *buffer.Buffer {
	label := name
	if label == "" {
		label = "Untitled"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The buffer %s has been closed.\n", label)
	if reopenableName(name) {
		fmt.Fprintf(&b, "\n[[%s|Re-open]]\n", name)
	}
	buf := e.lib.NewFromString(b.String())
	buf.SetFilename(closedPlaceholderURL)
	return buf
}

// renderClosed is the "closed" surface's render function. Tombstones are baked
// with their buffer's name at close time (newClosedPlaceholder), so this generic
// form is only reached if mew:/closed is opened directly, which nothing does.
func (e *Editor) renderClosed() string {
	return "This buffer has been closed.\n"
}

// followClosedReopen is the "closed" surface's follow handler: the Re-open link's
// target is the closed buffer's filename, so re-load it (reusing an open copy if
// one exists) and swap it into the placeholder's viewport in place — the
// tombstone becomes the reopened document.
func (e *Editor) followClosedReopen(target string) bool {
	target = strings.TrimSpace(target)
	w := e.surfaceTargetViewport()
	if w == nil || target == "" {
		return false
	}
	buf := e.findOpenBuffer(target)
	if buf == nil {
		loaded, err := e.loadBuffer(target)
		if err != nil {
			e.ShowError("Re-open " + displayPath(target) + ": " + err.Error())
			e.RequestRender()
			return true
		}
		buf = loaded
	}
	e.swapBuffer(w, buf)
	e.ensureCursorVisible(w)
	e.ShowNotificationTagged("→ "+displayPath(target), "navigate")
	e.RequestRender()
	return true
}
