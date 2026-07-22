package core

import "fmt"

// Version is the KittyTK major.minor release number and the single source of
// truth for it: bump it here (and the README) to cut a release. The third
// component of the full version is the build counter (see Build); use
// FullVersion for the complete major.minor.build string. Anything that needs
// to report the version - About dialogs, banners, handshake strings - reads it
// from here rather than hard-coding a literal.
const Version = "0.1"

// Build is the per-commit build counter and the third (patch) component of the
// full version: full version = major.minor.build. Unlike Version (a hand-set
// release number), it is bumped automatically by `make increment`, whose awk
// script rewrites the single `const Build = N` line below - so keep it on its
// own line in exactly that form.
const Build = 3

// FullVersion returns the complete version string, major.minor.build (e.g.
// "0.1.1"), assembled from Version and Build.
func FullVersion() string {
	return fmt.Sprintf("%s.%d", Version, Build)
}

// Name is the stylized product wordmark; Tagline is the recursive-acronym
// expansion of it (see the README). About dialogs and banners read the
// product identity from here so it lives in one place.
const (
	Name    = "KittyTK"
	Tagline = "image/tty Trinket Kit"
)
