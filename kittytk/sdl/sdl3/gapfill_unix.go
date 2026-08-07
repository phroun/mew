//go:build sdl && !windows

package sdl3

import "github.com/ebitengine/purego"

// openSDLForGaps reopens libSDL3 for the gap symbols to bind against. dlopen is
// refcounted, so this returns the handle the binding already loaded — but it
// has to find the file by the same widened search, since Homebrew's prefixes
// are not on dyld's default path.
func openSDLForGaps() uintptr {
	for _, name := range libraryCandidates() {
		if h, err := purego.Dlopen(name, purego.RTLD_LAZY|purego.RTLD_GLOBAL); err == nil {
			return h
		}
	}
	return 0
}

// gapSymbol looks up a symbol, returning 0 when absent so the caller can probe
// before binding (RegisterFunc panics on a missing one).
func gapSymbol(lib uintptr, name string) uintptr {
	if sym, err := purego.Dlsym(lib, name); err == nil {
		return sym
	}
	return 0
}
