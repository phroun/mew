//go:build sdl && darwin

package sdl

// This file holds only the //export callback. cgo requires that a file using
// //export keep its C preamble to declarations only, so the Objective-C
// definitions live in aboutmenu_darwin.go instead.

import "C"

//export kittytkAboutClicked
func kittytkAboutClicked() {
	if macAboutHandler != nil {
		macAboutHandler()
	}
}
