//go:build !sdl

package config

// hostIsGraphical is false for every build but the graphical host's — the plain
// terminal editor and the KittyTK TUI host alike. See keyhints_gfx.go.
const hostIsGraphical = false
