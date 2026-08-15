// Command spawndesktop is a throwaway display-protocol client that asks a
// running display service to reveal a desktop, then exits. It links only
// client + protocol (no rendering).
//
// When a solo app owns the whole display, running this converts it into an
// ordinary torn-off, dockable window with a desktop behind it - without the
// solo app having to cooperate. Any process on the protocol can request the
// same; this is just the smallest possible one.
//
//	# desktop is currently a solo app filling the screen
//	go run ./examples/spawndesktop           # reveal the desktop
//	go run ./examples/spawndesktop -solo      # the inverse: go back to solo
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/phroun/kittytk/client"
)

func main() {
	toSolo := flag.Bool("solo", false, "promote a detached app back to solo instead of revealing a desktop")
	flag.Parse()

	verb := "spawndesktop"
	if *toSolo {
		verb = "gosolo"
	}

	path := client.DefaultSocketPath()
	conn, err := client.Dial(path, "spawndesktop", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach display service at %s: %v\n", path, err)
		os.Exit(1)
	}
	defer conn.Close()

	if _, err := conn.Exec(verb); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", verb, err)
		os.Exit(1)
	}
}
