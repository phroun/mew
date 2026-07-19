//go:build kittytk

package main

import (
	"fmt"
	"os"

	"github.com/phroun/mew-app/internal/selfinstall"
)

// maybeInstall handles the self-installer flags (--install / --uninstall) for
// the terminal binary. It returns (exitCode, true) when it consumed one, so
// main() exits without starting the editor. The actual work lives in
// internal/selfinstall (Windows and macOS; a stub elsewhere). The graphical
// build reaches the same code through its first-run welcome window instead.
func maybeInstall(args []string) (int, bool) {
	for _, a := range args {
		switch a {
		case "--install", "-install":
			if !selfinstall.Available() {
				fmt.Fprintln(os.Stderr, "mew: --install is only supported on Windows and macOS")
				return 2, true
			}
			if _, err := selfinstall.Install(); err != nil {
				fmt.Fprintf(os.Stderr, "mew: install failed: %v\n", err)
				return 1, true
			}
			fmt.Println("mew installed")
			return 0, true
		case "--uninstall", "-uninstall":
			if !selfinstall.Available() {
				fmt.Fprintln(os.Stderr, "mew: --uninstall is only supported on Windows and macOS")
				return 2, true
			}
			if err := selfinstall.Uninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "mew: uninstall failed: %v\n", err)
				return 1, true
			}
			fmt.Println("mew uninstalled")
			return 0, true
		}
	}
	return 0, false
}
