// Module mew-app holds the shippable mew applications: the command-line mew
// (a KittyTK TUI-mode host presenting a root editor) and mew-sdl (the graphical
// host). Both build on the mew core editor library (github.com/phroun/mew) and,
// as they grow, the vendored KittyTK fork (github.com/phroun/kittytk). Wired to
// the sibling modules by the repository go.work.
module github.com/phroun/mew-app

go 1.25.0

require github.com/phroun/mew v0.3.1-alpha

require (
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/phroun/argwild v0.0.1 // indirect
	github.com/phroun/direct-key-handler v0.3.7 // indirect
	github.com/phroun/garland v0.1.8 // indirect
	github.com/phroun/pawscript v0.2.11-alpha // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
