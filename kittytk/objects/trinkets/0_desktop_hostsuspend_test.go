package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// suspendingBackend is a graphical backend that can also suspend, counting
// how often it was asked to.
type suspendingBackend struct {
	*raster.Backend
	asked int
}

func (b *suspendingBackend) SuspendHost() bool { b.asked++; return true }

// host_suspend reaches a backend that can suspend.
func TestHostSuspendReachesABackendThatCanSuspend(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(320, 200)
	be := &suspendingBackend{Backend: px}
	d := NewDesktop()
	d.SetBackend(be)

	if !d.HandleResolvedCommand(core.CmdHostSuspend, "^Z") {
		t.Error("host_suspend was not taken")
	}
	if be.asked != 1 {
		t.Errorf("SuspendHost called %d times, want 1", be.asked)
	}
}

// On a backend that cannot suspend (the graphical one) the command is still
// taken and does nothing, so a key bound to it never falls through to whatever
// has focus.
func TestHostSuspendIsANoOpWhereTheBackendCannotSuspend(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(320, 200)
	d := NewDesktop()
	d.SetBackend(px)

	if !d.HandleResolvedCommand(core.CmdHostSuspend, "^Z") {
		t.Error("host_suspend fell through on a backend that cannot suspend")
	}
}
