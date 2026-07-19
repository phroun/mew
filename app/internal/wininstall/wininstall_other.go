//go:build !windows

// Non-Windows stub for the Windows self-installer. The installer only makes
// sense on Windows (Start Menu shortcut, per-user PATH, registry first-run flag),
// so everywhere else it reports itself unavailable, treats the first run as
// already done (so the graphical host never shows the Windows welcome window),
// and fails loudly if asked to install.
package wininstall

import "errors"

// Available reports whether the installer works on this platform (false here).
func Available() bool { return false }

// FirstRunDone is true off Windows so no first-run welcome is ever shown.
func FirstRunDone() bool { return true }

// Install is unavailable off Windows.
func Install() (string, error) { return "", errors.New("the installer is Windows-only") }

// Uninstall is unavailable off Windows.
func Uninstall() error { return errors.New("the installer is Windows-only") }
