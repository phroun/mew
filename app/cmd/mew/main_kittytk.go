//go:build kittytk

// Command mew, KittyTK host build (-tags kittytk).
//
// This build fronts the same mew editor with a KittyTK TUI host: it opens a
// maximized window holding a root-level mew editor and serves the KittyTK
// protocol, so other apps can connect and embed their own (sub-)mew editor
// trinkets. The plain build (no tags) keeps mew driving the terminal directly
// and remains the reference for comparison.
//
// Scaffold only for now — the host bootstrap and the unified mew/KittyTK
// argument parser land next. When it imports the KittyTK fork's editor trinket,
// the real (mew-backed) variant is selected by also passing -tags mew, i.e.
// `go build -tags "kittytk mew"`; the fork's placeholder is the -tags mew
// counterpart's absence.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/mew"
)

const usage = `mew edits words (KittyTK host build)

Usage:
  mew [options] [file ...]

This build hosts mew inside a KittyTK TUI window and serves the KittyTK
protocol. The host bootstrap is not wired yet; use the plain build (no
-tags kittytk) for the standalone terminal editor.

  -v, --version           print version and exit
  -h, --help              print this help and exit
`

func main() {
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v":
			fmt.Printf("mew %s (kittytk host)\n", mew.FullVersion())
			return
		case "--help", "-h":
			fmt.Print(usage)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "mew: KittyTK host build is a scaffold (not yet implemented); build without -tags kittytk for the terminal editor")
	os.Exit(1)
}
