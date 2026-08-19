// Module mew-app holds the shippable mew applications: the command-line mew
// (a KittyTK TUI-mode host presenting a root editor) and mew-sdl (the graphical
// host). Both build on the mew core editor library (github.com/phroun/mew) and,
// as they grow, the vendored KittyTK fork (github.com/phroun/kittytk). Wired to
// the sibling modules by the repository go.work.
module github.com/phroun/mew-app

go 1.25.0

require (
	github.com/phroun/kittytk v0.1.24-alpha
	github.com/phroun/mew v0.3.1-alpha
	golang.org/x/sys v0.47.0
)

require (
	github.com/Zyko0/go-sdl3 v0.1.1 // indirect
	github.com/Zyko0/purego-gen v0.0.0-20250727121216-3bcd331a1e0c // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/go-text/render v0.2.1 // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/go-webgpu/goffi v0.6.2 // indirect
	github.com/go-webgpu/webgpu v0.5.4 // indirect
	github.com/gogpu/gpucontext v0.24.0 // indirect
	github.com/gogpu/gputypes v0.5.1 // indirect
	github.com/gogpu/naga v0.17.16 // indirect
	github.com/gogpu/wgpu v0.30.32 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/phroun/argwild v0.0.1 // indirect
	github.com/phroun/direct-key-handler v0.3.32 // indirect
	github.com/phroun/garland v0.1.11 // indirect
	github.com/phroun/key-sequence-processor v0.1.10-0.20260819113608-e19b44368555 // indirect
	github.com/phroun/pawscript v0.2.12-alpha // indirect
	github.com/phroun/purfecterm v0.2.52-0.20260819054657-0a89d0ec87be // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
