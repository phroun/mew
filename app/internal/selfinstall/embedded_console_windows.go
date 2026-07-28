//go:build windows && embedconsole

package selfinstall

import _ "embed"

// consoleGz is the console binary (mew.exe), gzipped, carried inside the
// graphical one. The build puts it here; see payload/README.md.
//
// It is behind a tag because a //go:embed of a missing file does not compile,
// and the file is a build artefact that is not in the repository. Without the
// tag the package builds and simply has nothing to offer.
//
//go:embed payload/mew.exe.gz
var consoleGz []byte

func embeddedConsole() []byte { return consoleGz }
