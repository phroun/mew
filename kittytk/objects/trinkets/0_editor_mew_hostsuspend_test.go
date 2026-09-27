//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// mew's host_suspend reaches the backend driving the editor's desktop when that
// backend can suspend, and reports false - so the command fails and a binding
// falls through - when there is no desktop or the backend cannot suspend.
func TestHostSuspendReachesTheDesktopsBackend(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	e := NewEditor()
	defer e.Close()
	if e.hostSuspend() {
		t.Error("an editor on no desktop reported a suspend")
	}

	px, _ := raster.New(320, 200)
	be := &suspendingBackend{Backend: px}
	d := NewDesktop()
	d.SetBackend(be)
	e.SetParent(d)
	if got := e.hostSuspend(); !got || be.asked != 1 {
		t.Errorf("suspending backend: reported=%v asked=%d, want true and 1", got, be.asked)
	}

	plain, _ := raster.New(320, 200)
	d.SetBackend(plain)
	if e.hostSuspend() {
		t.Error("a backend that cannot suspend reported a suspend")
	}
}
