// Module mew-app holds the shippable mew applications: the command-line mew
// (a KittyTK TUI-mode host presenting a root editor) and mew-sdl (the graphical
// host). Both build on the mew core editor library (github.com/phroun/mew) and,
// as they grow, the vendored KittyTK fork (github.com/phroun/kittytk). Wired to
// the sibling modules by the repository go.work.
module github.com/phroun/mew-app

go 1.25.0

require (
	github.com/phroun/kittytk v0.1.3-alpha
	github.com/phroun/mew v0.3.1-alpha
	golang.org/x/sys v0.47.0
)

require (
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/go-text/render v0.2.1 // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/phroun/argwild v0.0.1 // indirect
	github.com/phroun/direct-key-handler v0.3.7 // indirect
	github.com/phroun/garland v0.1.9 // indirect
	github.com/phroun/pawscript v0.2.11-alpha // indirect
	github.com/phroun/purfecterm v0.2.22 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/veandco/go-sdl2 v0.4.40 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
