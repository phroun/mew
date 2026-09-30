//go:build sdl && darwin

package sdl

// This file holds only the //export callbacks. cgo requires that a file using
// //export keep its C preamble to declarations only, so the Objective-C
// definitions live in nativemenu_darwin.go instead.

/*
#include <stdint.h>
*/
import "C"

//export kittytkMenuItemChosen
func kittytkMenuItemChosen(tag C.long) {
	nativeItemChosen(int(tag))
}

//export kittytkMenuItemValid
func kittytkMenuItemValid(tag C.long) C.int {
	if nativeItemEnabled(int(tag)) {
		return 1
	}
	return 0
}

//export kittytkMenuNeedsUpdate
func kittytkMenuNeedsUpdate(menu C.uintptr_t) {
	fillNativeMenu(cocoaMenuSink{}, uintptr(menu))
}
