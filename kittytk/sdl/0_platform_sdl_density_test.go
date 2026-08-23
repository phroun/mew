//go:build sdl

package sdl

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
)

// A configured density reaches every surface the platform builds, and survives
// what SDL says about the screen.
//
// The auto path can only run once a window exists, which is after a backend may
// already have been made — so the two orders both have to work, and an explicit
// setting has to win over the later discovery rather than the last writer.
func TestConfiguredDensityReachesBackendsAndBeatsAutoDetect(t *testing.T) {
	p := &Platform{scale: 2}
	p.SetDisplayDensity(2.0)

	// A surface built AFTER the setting picks it up.
	b, err := raster.NewScaled(320, 200, p.scale)
	if err != nil {
		t.Fatal(err)
	}
	p.applyMetrics(b)
	if got := b.DisplayDensity(); got != 2 {
		t.Errorf("a surface built after the setting has density %v, want 2", got)
	}

	// A surface built BEFORE it is corrected in place.
	p2 := &Platform{scale: 2}
	early, err := raster.NewScaled(320, 200, p2.scale)
	if err != nil {
		t.Fatal(err)
	}
	p2.backend = early
	p2.SetDisplayDensity(2.0)
	if got := early.DisplayDensity(); got != 2 {
		t.Errorf("a surface built before the setting has density %v, want 2", got)
	}

	// And the window system does not get to overrule the user afterwards.
	// adoptWindowDensity is what runs when a window opens; a configured
	// platform must ignore it. (Passing nil stands for "SDL had no answer",
	// which must equally leave the setting alone.)
	p.adoptWindowDensity(nil)
	if p.density != 2 || !p.densitySet {
		t.Errorf("density = %v (set=%v) after a window opened, want the configured 2",
			p.density, p.densitySet)
	}
}

// Zero means auto, not "a density of zero": it must leave the backend saying
// "unknown" so the Painter's default of 1 stands, rather than pinning a number
// nothing measured.
func TestZeroDensityLeavesItUnknown(t *testing.T) {
	p := &Platform{scale: 1}
	p.SetDisplayDensity(0)
	if p.densitySet {
		t.Error("zero was taken as an explicit setting; it means auto")
	}
	b, err := raster.NewScaled(320, 200, p.scale)
	if err != nil {
		t.Fatal(err)
	}
	p.applyMetrics(b)
	if got := b.DisplayDensity(); got != 0 {
		t.Errorf("density = %v with nothing configured, want 0 (unknown)", got)
	}
}
