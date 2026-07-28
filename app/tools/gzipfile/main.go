// gzipfile compresses one file into another, for the build.
//
// In Go rather than a shell pipeline because the Makefile's Windows targets
// are run ON Windows, where gzip is not something to assume. A toolchain that
// can build mew can run this.
package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gzipfile <src> <dst.gz>")
		os.Exit(2)
	}
	in, err := os.Open(os.Args[1])
	check(err)
	defer in.Close()

	out, err := os.Create(os.Args[2])
	check(err)
	defer out.Close()

	zw, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	check(err)
	_, err = io.Copy(zw, in)
	check(err)
	check(zw.Close())
	check(out.Close())

	si, err := os.Stat(os.Args[1])
	check(err)
	so, err := os.Stat(os.Args[2])
	check(err)
	fmt.Printf("gzipped %s: %d -> %d bytes\n", os.Args[1], si.Size(), so.Size())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gzipfile:", err)
		os.Exit(1)
	}
}
