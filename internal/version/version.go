// Package version is the single source of truth for mew's version and
// product identity, mirroring the KittyTK versioning system: a hand-set
// major.minor release number plus an auto-incremented build counter.
package version

import "fmt"

// Version is the mew major.minor release number and the single source of
// truth for it: bump it here (and the README) to cut a release. The third
// component of the full version is the build counter (see Build); use
// FullVersion for the complete major.minor.build string. Anything that needs
// to report the version - banners, --version, host handshakes - reads it
// from here rather than hard-coding a literal.
const Version = "0.3"

// Build is the per-commit build counter and the third (patch) component of the
// full version: full version = major.minor.build. Unlike Version (a hand-set
// release number), it is bumped automatically by `make increment`, whose awk
// script rewrites the single `const Build = N` line below - so keep it on its
// own line in exactly that form.
const Build = 2

// FullVersion returns the complete version string, major.minor.build (e.g.
// "0.3.1"), assembled from Version and Build.
func FullVersion() string {
	return fmt.Sprintf("%s.%d", Version, Build)
}

// Name is the product name; Tagline is its expansion (see the README).
// Banners and About surfaces read the product identity from here so it lives
// in one place.
const (
	Name    = "mew"
	Tagline = "mew edits words"
)

// Banner is the launch greeting: product identity, version, and the two keys a
// stranded user needs first. The two keys are TFC codes (Text Format Control):
// shown through the transient's TFC-expanding path they resolve to the LIVE
// bindings, spelled out for beginners and colored; shown raw (or where TFC is
// not expanded) they simply read as the codes. The %…% key references are
// passed as %s arguments so fmt does not try to interpret them.
func Banner() string {
	const (
		cancelKey = "%keys_verbose#nav_cancel.cancel.buffer_close|^C%"
		helpKey   = "%keys_verbose#help_toggle|^Q H%"
	)
	return fmt.Sprintf("%s %s build %d ** Type %s to close or %s for help.",
		Tagline, Version, Build, cancelKey, helpKey)
}
