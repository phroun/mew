//go:build sdl && darwin

package sdl

// This file holds only the //export callbacks. cgo requires that a file using
// //export keep its C preamble to declarations only, so the Objective-C
// definitions live in dockmenu_darwin.go instead.

/*
#include <stdint.h>
*/
import "C"

//export kittytkBuildDockMenu
func kittytkBuildDockMenu() C.uintptr_t {
	return C.uintptr_t(buildDockMenu(cocoaDockSink{}))
}

//export kittytkDockItemChosen
func kittytkDockItemChosen(tag C.long) {
	dockItemChosen(int(tag))
}
