// Package selfinstall is mew's own installer: a portable copy of mew run from a
// download can install itself, with no external installer framework. It decides
// "already installed" from where the binary lives (FirstRunDone) rather than a
// stored flag, and everything is per-user (no elevation prompt).
//
// It is implemented per platform — Windows (Start Menu shortcut + PATH under
// %LOCALAPPDATA%/HKCU) and macOS (the .app bundle copied into an Applications
// folder); every other platform gets a stub reporting itself unavailable.
// Available reports whether this build can install; FirstRunDone whether this
// copy already looks installed; InstallLocationPhrase describes the destination
// for the welcome copy. Both `mew --install` and the graphical first-run welcome
// window call Install().
package selfinstall
