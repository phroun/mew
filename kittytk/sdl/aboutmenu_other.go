//go:build sdl && !darwin

package sdl

// installAboutMenuHandler is a no-op off macOS: only macOS has a native
// application menu whose About item needs retargeting.
func installAboutMenuHandler() {}
