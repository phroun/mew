//go:build sdl && darwin

package sdl

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdint.h>
#include <stdlib.h>
#include <objc/runtime.h>
#include <objc/message.h>

// Defined in Go via //export (see dockmenu_export_darwin.go).
extern uintptr_t kittytkBuildDockMenu(void);
extern void kittytkDockItemChosen(long tag);

// kt_dock_menu_imp answers the application delegate's applicationDockMenu:,
// which AppKit asks each time the Dock is about to show the icon's menu. The
// menu comes back autoreleased, as the method's contract has it; nil leaves
// the Dock showing only its own items.
static id kt_dock_menu_imp(id self, SEL _cmd, id sender) {
	return (id)kittytkBuildDockMenu();
}

// kt_dock_item_imp is every item's action: the item's tag names what to do.
static void kt_dock_item_imp(id self, SEL _cmd, id sender) {
	long tag = ((long (*)(id, SEL))objc_msgSend)(sender, sel_registerName("tag"));
	kittytkDockItemChosen(tag);
}

static id kt_nsstring(const char *s) {
	return ((id (*)(id, SEL, const char *))objc_msgSend)(
		(id)objc_getClass("NSString"), sel_registerName("stringWithUTF8String:"), s);
}

// kt_dock_target is the one object every item's action is sent to. Built
// once and kept.
static id kt_dock_target(void) {
	static id target = 0;
	if (target) return target;
	Class cls = objc_getClass("KittyTKDockTarget");
	if (!cls) {
		cls = objc_allocateClassPair(objc_getClass("NSObject"), "KittyTKDockTarget", 0);
		if (!cls) return 0;
		class_addMethod(cls, sel_registerName("kittytkDockItem:"), (IMP)kt_dock_item_imp, "v@:@");
		objc_registerClassPair(cls);
	}
	id t = ((id (*)(id, SEL))objc_msgSend)((id)cls, sel_registerName("alloc"));
	target = ((id (*)(id, SEL))objc_msgSend)(t, sel_registerName("init"));
	return target;
}

// kt_dock_new_menu makes an empty, autoreleased menu whose items keep the
// enabled state they are given rather than AppKit working it out.
static uintptr_t kt_dock_new_menu(void) {
	id m = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenu"), sel_registerName("alloc"));
	m = ((id (*)(id, SEL, id))objc_msgSend)(m, sel_registerName("initWithTitle:"), kt_nsstring(""));
	m = ((id (*)(id, SEL))objc_msgSend)(m, sel_registerName("autorelease"));
	((void (*)(id, SEL, signed char))objc_msgSend)(m, sel_registerName("setAutoenablesItems:"), 0);
	return (uintptr_t)m;
}

// kt_dock_add_item appends an item to menu and returns it. A tag above zero
// makes the item send its action to the shared target; mark 1 is a tick.
//
// The Dock draws the menu itself, from what AppKit hands it, and draws the
// standard tick but not a state image of ours -- a diamond set that way never
// appeared -- so a tick is the only mark there is.
static uintptr_t kt_dock_add_item(uintptr_t menu, const char *title, long tag, int enabled, int mark, long indent) {
	SEL action = tag > 0 ? sel_registerName("kittytkDockItem:") : (SEL)0;
	id item = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("alloc"));
	item = ((id (*)(id, SEL, id, SEL, id))objc_msgSend)(item,
		sel_registerName("initWithTitle:action:keyEquivalent:"), kt_nsstring(title), action, kt_nsstring(""));
	if (tag > 0) {
		((void (*)(id, SEL, id))objc_msgSend)(item, sel_registerName("setTarget:"), kt_dock_target());
		((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setTag:"), tag);
	}
	((void (*)(id, SEL, signed char))objc_msgSend)(item, sel_registerName("setEnabled:"), enabled ? 1 : 0);
	((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setIndentationLevel:"), indent);
	if (mark == 1) {
		((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setState:"), 1);
	}
	((void (*)(id, SEL, id))objc_msgSend)((id)menu, sel_registerName("addItem:"), item);
	// The menu holds the item now; the reference alloc gave us is ours to drop.
	((void (*)(id, SEL))objc_msgSend)(item, sel_registerName("release"));
	return (uintptr_t)item;
}

static void kt_dock_add_separator(uintptr_t menu) {
	id sep = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("separatorItem"));
	((void (*)(id, SEL, id))objc_msgSend)((id)menu, sel_registerName("addItem:"), sep);
}

static void kt_dock_set_submenu(uintptr_t item, uintptr_t submenu) {
	((void (*)(id, SEL, id))objc_msgSend)((id)item, sel_registerName("setSubmenu:"), (id)submenu);
}

// kt_dock_install teaches the application's delegate -- SDL's -- to answer
// applicationDockMenu:. SDL does not implement it, so the method is added to
// the delegate's class; should a later SDL implement it, ours replaces it.
//
// AppKit may already have noted, when the delegate was set, which of the
// optional methods it answers. So once the method is newly added, the
// delegate is set again, and AppKit looks afresh. Idempotent, and a no-op
// while there is no application or no delegate yet.
static void kt_dock_install(void) {
	id app = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (!app) return;
	id delegate = ((id (*)(id, SEL))objc_msgSend)(app, sel_registerName("delegate"));
	if (!delegate) return;
	Class cls = object_getClass(delegate);
	SEL sel = sel_registerName("applicationDockMenu:");
	if (class_getMethodImplementation(cls, sel) == (IMP)kt_dock_menu_imp) return;
	if (class_addMethod(cls, sel, (IMP)kt_dock_menu_imp, "@@:@")) {
		((void (*)(id, SEL, id))objc_msgSend)(app, sel_registerName("setDelegate:"), (id)0);
		((void (*)(id, SEL, id))objc_msgSend)(app, sel_registerName("setDelegate:"), delegate);
	} else {
		class_replaceMethod(cls, sel, (IMP)kt_dock_menu_imp, "@@:@");
	}
}
*/
import "C"

import (
	"unsafe"

	"github.com/phroun/kittytk/platform"
)

// installDockMenu makes the Dock ask us for the icon's menu. Safe to call
// more than once, and before the application is up (a no-op then).
func installDockMenu() {
	C.kt_dock_install()
}

// cocoaDockSink writes a Dock menu into AppKit.
type cocoaDockSink struct{}

func (cocoaDockSink) newMenu() uintptr { return uintptr(C.kt_dock_new_menu()) }

func (cocoaDockSink) addItem(menu uintptr, title string, tag int, enabled bool, mark platform.DockMark, indent int) uintptr {
	cs := C.CString(title)
	defer C.free(unsafe.Pointer(cs))
	en := C.int(0)
	if enabled {
		en = 1
	}
	return uintptr(C.kt_dock_add_item(C.uintptr_t(menu), cs, C.long(tag), en, C.int(mark), C.long(indent)))
}

func (cocoaDockSink) addSeparator(menu uintptr) { C.kt_dock_add_separator(C.uintptr_t(menu)) }

func (cocoaDockSink) setSubmenu(item, submenu uintptr) {
	C.kt_dock_set_submenu(C.uintptr_t(item), C.uintptr_t(submenu))
}
