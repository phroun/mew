//go:build sdl

package sdl

import (
	"os"
	"testing"
	"time"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// EVERY window must have text input started, not just the first.
//
// SDL3 scopes text input to a window and leaves it off until asked;
// SDL2's SDL_StartTextInput() was global and on by default. The port
// carried one call for the main window, so every window created
// afterwards — every torn-off window — received key events but no
// SDL_EVENT_TEXT_INPUT. Tab and the arrows kept working (a separate
// stream that is always on) and only typing was lost, which is a
// symptom easy to blame on focus.
func TestEveryWindowStartsTextInput(t *testing.T) {
	requireSDL(t)
	os.Setenv("SDL_VIDEODRIVER", "dummy")

	p := newTestPlatform(t)

	var mainActive, secondActive bool
	var checked bool

	done := make(chan int, 1)
	go func() {
		done <- p.Run(func(pf platform.Platform) {
			if _, err := pf.CreateSurface(platform.SurfaceOptions{}); err != nil {
				t.Errorf("CreateSurface (main): %v", err)
				pf.Quit(1)
				return
			}
			// A second surface: the shape a torn-off window takes.
			if _, err := pf.CreateSurface(platform.SurfaceOptions{
				Title: "torn", WidthPx: 200, HeightPx: 120, Borderless: true,
			}); err != nil {
				t.Errorf("CreateSurface (second): %v", err)
				pf.Quit(1)
				return
			}

			pf.PostAfter(50*time.Millisecond, func() {
				var seen int
				for _, w := range p.wins {
					if w.window == nil {
						continue
					}
					active := sdl3.TextInputActive(w.window)
					if w == p.main {
						mainActive = active
					} else {
						secondActive = active
					}
					seen++
				}
				checked = seen >= 2
				pf.Quit(7)
			})
		})
	}()

	select {
	case code := <-done:
		if code != 7 {
			t.Fatalf("exit code = %d, want 7", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SDL loop did not exit")
	}

	if !checked {
		t.Fatal("fewer than two windows existed; this test proves nothing")
	}
	if !mainActive {
		t.Error("the main window has no text input enabled")
	}
	if !secondActive {
		t.Error("a second window has no text input enabled — it would receive key " +
			"events but no typed text, exactly the torn-off window bug")
	}
}

// A typed character reaches the surface it was typed into, not the main
// one. Routing is by SDL window id; getting it wrong would send every
// torn-off window's typing to the desktop.
func TestTextInputRoutesToItsOwnSurface(t *testing.T) {
	requireSDL(t)
	os.Setenv("SDL_VIDEODRIVER", "dummy")

	p := newTestPlatform(t)

	mainKeys := &keyRecorder{}
	secondKeys := &keyRecorder{}

	done := make(chan int, 1)
	go func() {
		done <- p.Run(func(pf platform.Platform) {
			s1, err := pf.CreateSurface(platform.SurfaceOptions{})
			if err != nil {
				t.Errorf("CreateSurface (main): %v", err)
				pf.Quit(1)
				return
			}
			s1.SetHandler(mainKeys)

			s2, err := pf.CreateSurface(platform.SurfaceOptions{
				Title: "torn", WidthPx: 200, HeightPx: 120, Borderless: true,
			})
			if err != nil {
				t.Errorf("CreateSurface (second): %v", err)
				pf.Quit(1)
				return
			}
			s2.SetHandler(secondKeys)

			pf.PostAfter(50*time.Millisecond, func() {
				// Deliver text to the SECOND window's id directly, the way
				// the event loop would on a real keystroke there.
				var secondID uint32
				for id, w := range p.wins {
					if w != p.main {
						secondID = id
					}
				}
				if secondID == 0 {
					t.Error("no second window to route to")
					pf.Quit(1)
					return
				}
				if s := p.surfaceFor(secondID); s != nil && s.handler != nil {
					s.handler.Event(core.KeyPressEvent{Key: "x"})
				}
				pf.Quit(7)
			})
		})
	}()

	select {
	case code := <-done:
		if code != 7 {
			t.Fatalf("exit code = %d, want 7", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SDL loop did not exit")
	}

	if secondKeys.keys != 1 {
		t.Errorf("the second surface got %d key events, want 1", secondKeys.keys)
	}
	if mainKeys.keys != 0 {
		t.Errorf("the main surface got %d key events, want 0 — typing leaked across windows",
			mainKeys.keys)
	}
}

// keyRecorder counts key events delivered to a surface.
type keyRecorder struct{ keys int }

func (k *keyRecorder) Frame(*core.Painter) {}
func (k *keyRecorder) Event(ev core.Event) bool {
	if _, ok := ev.(core.KeyPressEvent); ok {
		k.keys++
	}
	return true
}
func (k *keyRecorder) Resized(core.UnitSize) {}
