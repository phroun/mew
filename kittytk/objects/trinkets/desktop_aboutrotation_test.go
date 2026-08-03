package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// rotGatePlatform records the rotation-trigger gate the desktop wires onto the
// platform, standing in for the SDL/WebGPU host's SetRotationTriggerGate.
type rotGatePlatform struct {
	*msPlatform
	gate func() bool
}

func (p *rotGatePlatform) Run(init func(platform.Platform)) int {
	init(p)
	if p.msPlatform.script != nil {
		p.msPlatform.script()
	}
	return 0
}

func (p *rotGatePlatform) SetRotationTriggerGate(fn func() bool) { p.gate = fn }

// The R-key rotation easter egg must be gated to the About KittyTK dialog: the
// desktop wires aboutBoxFocused onto the platform, and that predicate is true
// only while the About box is open AND the active window — so R does nothing in
// the editor, and stops rotating the moment focus leaves the dialog.
func TestAboutBoxRotationGate(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	plat := &rotGatePlatform{msPlatform: &msPlatform{}}
	plat.msPlatform.script = func() {
		if plat.gate == nil {
			t.Fatal("desktop did not wire the rotation gate onto the platform")
		}
		if plat.gate() {
			t.Fatal("gate is true with no About box open")
		}

		d.showAboutDesktop()
		if !plat.gate() {
			t.Fatal("gate is false while the About box is open and active")
		}

		// Focus moving off the dialog closes the gate (R must not keep the
		// screen spinning from another window).
		d.WindowManager().DeactivateActiveWindow()
		if plat.gate() {
			t.Fatal("gate stayed true after the About box lost focus")
		}

		// Closing the dialog clears the tracked reference.
		d.mu.RLock()
		ab := d.aboutBox
		d.mu.RUnlock()
		if ab == nil {
			t.Fatal("About box reference was dropped before close")
		}
		ab.Close()
		d.mu.RLock()
		cleared := d.aboutBox == nil
		d.mu.RUnlock()
		if !cleared {
			t.Fatal("About box reference not cleared on close")
		}
		if plat.gate() {
			t.Fatal("gate is true after the About box closed")
		}

		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}
