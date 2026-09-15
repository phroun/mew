package client_test

import (
	"go/build"
	"strings"
	"testing"
)

// An application that speaks the display protocol compiles this package, the
// wire language and serval, and nothing else of the toolkit -- no registry, no
// session, no trinkets, and so none of the dependencies those carry. That is
// what lets the shim ship on its own, and it is one import away from being
// untrue at any time: the registry's names are re-exported through package
// protocol, so reaching for one reads as harmless and is not.
//
// serval is here because an application serves RECORDS: what a Fill hands back
// is serval's shape, and an author who has records already has the library that
// says what a record is. It carries nothing of the toolkit with it.
//
// The in-process transport is the deliberate exception and lives in package
// inprocess for exactly this reason.
func TestClientImportsOnlyTheWire(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		head, _, _ := strings.Cut(imp, "/")
		if !strings.Contains(head, ".") {
			continue // standard library
		}
		if imp == "github.com/phroun/kittytk/wire" || imp == "github.com/phroun/serval" {
			continue
		}
		t.Errorf("client imports %s; it may import the wire language, serval "+
			"and the standard library only", imp)
	}
}
