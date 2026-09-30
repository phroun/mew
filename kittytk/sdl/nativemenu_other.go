//go:build sdl && !darwin

package sdl

// applyNativeMenus is a no-op off macOS: only macOS has a menu bar of the
// OS's own for the desktop's menus to go into.
func applyNativeMenus() {}
