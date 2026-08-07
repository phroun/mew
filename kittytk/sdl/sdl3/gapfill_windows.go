//go:build sdl && windows

package sdl3

import "golang.org/x/sys/windows"

// openSDLForGaps reopens libSDL3 for the gap symbols to bind against.
// LoadLibrary is refcounted, so reopening the same name the binding loaded
// (csdl.Path leads libraryCandidates on Windows — the exact file binsdl
// extracted and loaded) hands back that module rather than a second copy. The
// handle is held for the process lifetime, as dlopen's is on Unix.
func openSDLForGaps() uintptr {
	for _, name := range libraryCandidates() {
		if h, err := windows.LoadLibrary(name); err == nil && h != 0 {
			return uintptr(h)
		}
	}
	return 0
}

// gapSymbol looks up a symbol, returning 0 when absent so the caller can probe
// before binding (RegisterFunc panics on a missing one).
func gapSymbol(lib uintptr, name string) uintptr {
	addr, err := windows.GetProcAddress(windows.Handle(lib), name)
	if err != nil {
		return 0
	}
	return addr
}
