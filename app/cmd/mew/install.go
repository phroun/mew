//go:build kittytk

package main

import (
	"fmt"
	"os"

	"github.com/phroun/mew-app/internal/wininstall"
)

// maybeInstall handles the self-installer flags (--install / --uninstall) for
// the terminal binary. It returns (exitCode, true) when it consumed one, so
// main() exits without starting the editor. The actual work lives in
// internal/wininstall (Windows-only; a stub elsewhere). The graphical build
// reaches the same code through its first-run welcome window instead.
func maybeInstall(args []string) (int, bool) {
	for _, a := range args {
		switch a {
		case "--install", "-install":
			if !wininstall.Available() {
				fmt.Fprintln(os.Stderr, "mew: --install is only supported on Windows")
				return 2, true
			}
			if _, err := wininstall.Install(); err != nil {
				fmt.Fprintf(os.Stderr, "mew: install failed: %v\n", err)
				return 1, true
			}
			fmt.Println(`mew installed — find "mew" in the Start Menu, or run "mew" / "mew --window" from a console`)
			return 0, true
		case "--uninstall", "-uninstall":
			if !wininstall.Available() {
				fmt.Fprintln(os.Stderr, "mew: --uninstall is only supported on Windows")
				return 2, true
			}
			if err := wininstall.Uninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "mew: uninstall failed: %v\n", err)
				return 1, true
			}
			fmt.Println("mew uninstalled")
			return 0, true
		}
	}
	return 0, false
}
