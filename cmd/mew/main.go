// Command mew is a console-based text editor.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/mew"
)

const usage = `mew edits words

Usage:
  mew [options] [file ...]

Options are the long-form editor options (the [options] config keys), applied
left to right as files open, so an option before a file affects that file and
the files after it, and an option after a file changes only later files:

  --optionName            enable a boolean option    (also --optionName=on/1)
  --optionName-           disable a boolean option   (also --optionName=off/0)
  --optionName value      set a valued option        (also --optionName=value)
  +N                      open the next file at line N

  -v, --version           print version and exit
  -h, --help              print this help and exit

Truly global options must come before the first file.

Examples:
  mew notes.txt
  mew --syntax go --showLineNumbers main.go
  mew +42 --wordWrap draft.md readme.md
`

func main() {
	args := os.Args[1:]

	// --version / --help are terminal meta flags handled before launching.
	for _, a := range args {
		switch a {
		case "--version", "-v":
			fmt.Printf("mew %s\n", mew.FullVersion())
			return
		case "--help", "-h":
			fmt.Print(usage)
			return
		}
	}

	if err := mew.EditArgv(args); err != nil {
		fmt.Fprintf(os.Stderr, "mew: %v\n", err)
		os.Exit(1)
	}
}
