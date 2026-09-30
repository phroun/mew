//go:build sdl && !darwin

package sdl

// installDockMenu is a no-op off macOS: only macOS has a Dock whose icon
// carries a menu of the application's own.
func installDockMenu() {}
