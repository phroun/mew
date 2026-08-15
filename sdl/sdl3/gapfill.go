//go:build sdl

package sdl3

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Gap-filling: SDL3 entry points the binding declares but leaves as
// panic("not implemented"), plus one it registers internally without
// exporting. Rather than vendor or fork it, we bind them ourselves —
// dlopen is refcounted, so opening libSDL3 again hands back the handle
// the binding already loaded, and purego registers the symbols against
// it.
//
// These are not optional extras. Without them the host panics the
// first time it needs a native window handle (tearing a window off) or
// a Metal layer (any GPU window on macOS).

var (
	sdlMetalGetLayer   func(view uintptr) unsafe.Pointer
	sdlMetalCreateView func(window uintptr) uintptr
	sdlGetPointerProp  func(props uint32, name string, def uintptr) uintptr

	gapfillOnce sync.Once
)

// gapCandidates is the search list gapfill reopens SDL3 through. It leads with
// the EXACT path the binding loaded (loadedSDLPath), so the reopen lands on the
// same file — refcounted, the same handle — rather than re-resolving a bare
// name to a different SDL3 and loading a second copy (see loadedSDLPath). The
// rest of libraryCandidates follows only as a fallback for an embedded build,
// where no path was recorded.
func gapCandidates() []string {
	c := libraryCandidates()
	if loadedSDLPath != "" {
		c = append([]string{loadedSDLPath}, c...)
	}
	return c
}

// bindGaps runs lazily, after Init has opened libSDL3.
func bindGaps() {
	// Reopen the library the binding already loaded — the open is refcounted
	// on every platform, so this hands back the same module rather than a
	// second copy. The open and the symbol probe are platform-specific (dlopen
	// on Unix, LoadLibrary on Windows), so they live in gapfill_unix.go /
	// gapfill_windows.go; everything below is shared.
	lib := openSDLForGaps()
	if lib == 0 {
		return // no SDL3 present; Init reports it
	}

	// Registration panics on a missing symbol, so probe first: an SDL3
	// built without the Metal backend simply has no layer to hand back.
	bind := func(target any, symbol string) {
		if sym := gapSymbol(lib, symbol); sym != 0 {
			purego.RegisterFunc(target, sym)
		}
	}
	bind(&sdlMetalGetLayer, "SDL_Metal_GetLayer")
	bind(&sdlMetalCreateView, "SDL_Metal_CreateView")
	bind(&sdlGetPointerProp, "SDL_GetPointerProperty")
}

// metalCreateView attaches a CAMetalLayer-backed view to a window and
// returns it, or 0 where Metal is unavailable.
func metalCreateView(window uintptr) uintptr {
	gapfillOnce.Do(bindGaps)
	if sdlMetalCreateView == nil || window == 0 {
		return 0
	}
	return sdlMetalCreateView(window)
}

// metalGetLayer returns the CAMetalLayer behind an SDL Metal view, or
// nil when this build has no Metal support.
func metalGetLayer(view uintptr) unsafe.Pointer {
	gapfillOnce.Do(bindGaps)
	if sdlMetalGetLayer == nil || view == 0 {
		return nil
	}
	return sdlMetalGetLayer(view)
}

// pointerProperty reads a pointer-valued property — where SDL3 keeps
// native window handles — or 0 when unset.
func pointerProperty(props uint32, name string) uintptr {
	gapfillOnce.Do(bindGaps)
	if sdlGetPointerProp == nil || props == 0 {
		return 0
	}
	return sdlGetPointerProp(props, name, 0)
}
