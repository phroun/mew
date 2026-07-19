// Command editordemo is a display-protocol APPLICATION that places an
// `editor` trinket in a window. Built plain, that trinket is the vanilla
// placeholder; built with -tags mew (against mew's KittyTK fork) it is a
// full mew editor session — same declaration either way.
//
//	terminal 1:  go run -tags mew ./examples/demo        # the display service
//	terminal 2:  go run -tags mew ./examples/editordemo  # this app
//
// Drop the tags to exercise the placeholder instead.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/client"
)

func main() {
	path := client.DefaultSocketPath()

	commands := make(chan string, 8)
	conn, err := client.Dial(path, "Editor Demo", func(id string) { commands <- id })
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach display service at %s: %v\n", path, err)
		fmt.Fprintln(os.Stderr, "start the desktop first: go run -tags mew ./examples/demo")
		os.Exit(1)
	}
	defer conn.Close()

	_, err = conn.Build(`
w=new window title="Editor" x=64 y=64 width=640 height=400 children={
	ed=new editor value="Edit me.\nThe placeholder build is a stub; the mew build is a real editor." syntax=default wrap=default
}
`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n", err)
		os.Exit(1)
	}

	// The editor emits `commit` on session end (mew build) or on OK (placeholder).
	for range commands {
	}
}
