// Command spawndesktop is a throwaway display-protocol client that shows or
// hides a running display service's desktop, then exits. It links only client +
// protocol (no rendering).
//
// When one app's window is filling the whole display, showing the desktop
// converts it into an ordinary torn-off, dockable window with a desktop behind
// it - without that app having to cooperate. Any process on the protocol can
// ask; this is just the smallest possible one.
//
//	go run ./examples/spawndesktop           # show the desktop
//	go run ./examples/spawndesktop -hide     # hide it again
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/phroun/kittytk/client"
)

func main() {
	hide := flag.Bool("hide", false, "hide the desktop instead of showing it")
	flag.Parse()

	path := client.DefaultSocketPath()
	conn, err := client.Dial(path, "spawndesktop", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach display service at %s: %v\n", path, err)
		os.Exit(1)
	}
	defer conn.Close()

	// Whether the desktop is showing belongs to the display, so it is a
	// property of the host object -- mew's show_desktop and hide_desktop.
	prop := "desktop"
	if *hide {
		prop = "!desktop"
	}
	if err := conn.Host().Set(prop); err != nil {
		fmt.Fprintf(os.Stderr, "set host %s: %v\n", prop, err)
		os.Exit(1)
	}
}
