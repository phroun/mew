//go:build sdl && darwin

package sdl

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdint.h>
#include <stdlib.h>
#include <objc/runtime.h>
#include <objc/message.h>

// Exported by libobjc; what @autoreleasepool compiles to.
extern void *objc_autoreleasePoolPush(void);
extern void objc_autoreleasePoolPop(void *pool);

// Defined in Go via //export (see nativemenu_export_darwin.go).
extern void kittytkMenuItemChosen(long tag);
extern int kittytkMenuItemValid(long tag);
extern void kittytkMenuNeedsUpdate(uintptr_t menu);

// The tag every top-level item of ours carries in the main menu, so the last
// set can be found and taken out again. Items inside our menus use tags from
// 1 up, which name their entries.
#define KT_BAR_TAG 0x4B54544B

static id kt_str(const char *s) {
	return ((id (*)(id, SEL, const char *))objc_msgSend)(
		(id)objc_getClass("NSString"), sel_registerName("stringWithUTF8String:"), s);
}

static void kt_item_chosen_imp(id self, SEL _cmd, id sender) {
	long tag = ((long (*)(id, SEL))objc_msgSend)(sender, sel_registerName("tag"));
	kittytkMenuItemChosen(tag);
}

// validateMenuItem: is asked before an item is shown and before its key
// equivalent may fire; NO leaves the key to whatever has the focus.
static signed char kt_item_valid_imp(id self, SEL _cmd, id item) {
	long tag = ((long (*)(id, SEL))objc_msgSend)(item, sel_registerName("tag"));
	return kittytkMenuItemValid(tag) ? 1 : 0;
}

// menuNeedsUpdate: is asked before a menu is shown and before it is searched
// for a key equivalent: the moment to write its items from the desktop's own.
static void kt_menu_needs_update_imp(id self, SEL _cmd, id menu) {
	kittytkMenuNeedsUpdate((uintptr_t)menu);
}

static id kt_new_object(const char *className, void (*define)(Class)) {
	Class cls = objc_getClass(className);
	if (!cls) {
		cls = objc_allocateClassPair(objc_getClass("NSObject"), className, 0);
		if (!cls) return 0;
		define(cls);
		objc_registerClassPair(cls);
	}
	id o = ((id (*)(id, SEL))objc_msgSend)((id)cls, sel_registerName("alloc"));
	return ((id (*)(id, SEL))objc_msgSend)(o, sel_registerName("init"));
}

static void kt_define_target(Class cls) {
	class_addMethod(cls, sel_registerName("kittytkMenuItem:"), (IMP)kt_item_chosen_imp, "v@:@");
	class_addMethod(cls, sel_registerName("validateMenuItem:"), (IMP)kt_item_valid_imp, "c@:@");
}

static void kt_define_delegate(Class cls) {
	class_addMethod(cls, sel_registerName("menuNeedsUpdate:"), (IMP)kt_menu_needs_update_imp, "v@:@");
}

// The one object every item's action goes to, and the one delegate every
// menu of ours has. Built once and kept: a menu holds its delegate weakly.
static id kt_menu_target(void) {
	static id t = 0;
	if (!t) t = kt_new_object("KittyTKMenuTarget", kt_define_target);
	return t;
}

static id kt_menu_delegate(void) {
	static id d = 0;
	if (!d) d = kt_new_object("KittyTKMenuDelegate", kt_define_delegate);
	return d;
}

static void *kt_pool_push(void) { return objc_autoreleasePoolPush(); }
static void kt_pool_pop(void *p) { objc_autoreleasePoolPop(p); }

// kt_menu_new makes a menu with our delegate. The caller owns it until it is
// handed to kt_menu_set_submenu or kt_menu_bar_add, which take it over.
static uintptr_t kt_menu_new(const char *title) {
	id m = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenu"), sel_registerName("alloc"));
	m = ((id (*)(id, SEL, id))objc_msgSend)(m, sel_registerName("initWithTitle:"), kt_str(title));
	((void (*)(id, SEL, id))objc_msgSend)(m, sel_registerName("setDelegate:"), kt_menu_delegate());
	return (uintptr_t)m;
}

static void kt_menu_clear(uintptr_t menu) {
	((void (*)(id, SEL))objc_msgSend)((id)menu, sel_registerName("removeAllItems"));
}

// kt_menu_add_item appends an item and returns it (the menu holds it). A tag
// above zero sends the item's action, and its validation, to the shared
// target. equiv and mods are its key equivalent; an empty equiv binds none.
static uintptr_t kt_menu_add_item(uintptr_t menu, const char *title, long tag, int checked, const char *equiv, unsigned long mods) {
	SEL action = tag > 0 ? sel_registerName("kittytkMenuItem:") : (SEL)0;
	id item = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("alloc"));
	item = ((id (*)(id, SEL, id, SEL, id))objc_msgSend)(item,
		sel_registerName("initWithTitle:action:keyEquivalent:"), kt_str(title), action, kt_str(equiv));
	((void (*)(id, SEL, unsigned long))objc_msgSend)(item, sel_registerName("setKeyEquivalentModifierMask:"), mods);
	if (tag > 0) {
		((void (*)(id, SEL, id))objc_msgSend)(item, sel_registerName("setTarget:"), kt_menu_target());
		((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setTag:"), tag);
	}
	if (checked) {
		((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setState:"), 1);
	}
	((void (*)(id, SEL, id))objc_msgSend)((id)menu, sel_registerName("addItem:"), item);
	((void (*)(id, SEL))objc_msgSend)(item, sel_registerName("release"));
	return (uintptr_t)item;
}

static void kt_menu_add_separator(uintptr_t menu) {
	id sep = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("separatorItem"));
	((void (*)(id, SEL, id))objc_msgSend)((id)menu, sel_registerName("addItem:"), sep);
}

// kt_menu_set_submenu hangs submenu from item; the item holds it from here.
static void kt_menu_set_submenu(uintptr_t item, uintptr_t submenu) {
	((void (*)(id, SEL, id))objc_msgSend)((id)item, sel_registerName("setSubmenu:"), (id)submenu);
	((void (*)(id, SEL))objc_msgSend)((id)submenu, sel_registerName("release"));
}

static id kt_main_menu(void) {
	id app = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (!app) return 0;
	return ((id (*)(id, SEL))objc_msgSend)(app, sel_registerName("mainMenu"));
}

// kt_menu_bar_clear takes out the menus put in the bar last time.
static void kt_menu_bar_clear(void) {
	id main = kt_main_menu();
	if (!main) return;
	long n = ((long (*)(id, SEL))objc_msgSend)(main, sel_registerName("numberOfItems"));
	for (long i = n - 1; i >= 0; i--) {
		id it = ((id (*)(id, SEL, long))objc_msgSend)(main, sel_registerName("itemAtIndex:"), i);
		long tag = ((long (*)(id, SEL))objc_msgSend)(it, sel_registerName("tag"));
		if (tag == KT_BAR_TAG) {
			((void (*)(id, SEL, long))objc_msgSend)(main, sel_registerName("removeItemAtIndex:"), i);
		}
	}
}

// kt_menu_bar_add puts menu in the bar at position index among ours, after
// the application menu, taking it over. With no bar, the menu is let go.
static void kt_menu_bar_add(uintptr_t menu, const char *title, long index) {
	id main = kt_main_menu();
	if (!main) {
		((void (*)(id, SEL))objc_msgSend)((id)menu, sel_registerName("release"));
		return;
	}
	id item = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("alloc"));
	item = ((id (*)(id, SEL, id, SEL, id))objc_msgSend)(item,
		sel_registerName("initWithTitle:action:keyEquivalent:"), kt_str(title), (SEL)0, kt_str(""));
	((void (*)(id, SEL, long))objc_msgSend)(item, sel_registerName("setTag:"), (long)KT_BAR_TAG);
	((void (*)(id, SEL, id))objc_msgSend)((id)menu, sel_registerName("setTitle:"), kt_str(title));
	((void (*)(id, SEL, id))objc_msgSend)(item, sel_registerName("setSubmenu:"), (id)menu);
	((void (*)(id, SEL))objc_msgSend)((id)menu, sel_registerName("release"));
	long n = ((long (*)(id, SEL))objc_msgSend)(main, sel_registerName("numberOfItems"));
	long at = 1 + index;
	if (at > n) at = n;
	((void (*)(id, SEL, id, long))objc_msgSend)(main, sel_registerName("insertItem:atIndex:"), item, at);
	((void (*)(id, SEL))objc_msgSend)(item, sel_registerName("release"));
}
*/
import "C"

import (
	"unsafe"
)

// applyNativeMenus writes the latest menus into the macOS menu bar. It runs
// from the host's own loop rather than inside an AppKit callback, so it
// brings its own autorelease pool.
func applyNativeMenus() {
	nativeMenus.mu.Lock()
	menus := nativeMenus.set
	have := nativeMenus.haveSet
	nativeMenus.mu.Unlock()
	if !have {
		return
	}
	pool := C.kt_pool_push()
	defer C.kt_pool_pop(pool)
	setNativeBar(cocoaMenuSink{}, menus)
}

// cocoaMenuSink writes the native menu bar into AppKit.
type cocoaMenuSink struct{}

func (cocoaMenuSink) newMenu(title string) uintptr {
	cs := C.CString(title)
	defer C.free(unsafe.Pointer(cs))
	return uintptr(C.kt_menu_new(cs))
}

func (cocoaMenuSink) clear(menu uintptr) { C.kt_menu_clear(C.uintptr_t(menu)) }

func (cocoaMenuSink) addItem(menu uintptr, title string, tag int, checked bool, equiv string, mods uint) uintptr {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	ce := C.CString(equiv)
	defer C.free(unsafe.Pointer(ce))
	ch := C.int(0)
	if checked {
		ch = 1
	}
	return uintptr(C.kt_menu_add_item(C.uintptr_t(menu), ct, C.long(tag), ch, ce, C.ulong(mods)))
}

func (cocoaMenuSink) addSeparator(menu uintptr) { C.kt_menu_add_separator(C.uintptr_t(menu)) }

func (cocoaMenuSink) setSubmenu(item, submenu uintptr) {
	C.kt_menu_set_submenu(C.uintptr_t(item), C.uintptr_t(submenu))
}

func (cocoaMenuSink) setBar(menus []uintptr, titles []string) {
	// With no bar, kt_menu_bar_add lets each menu go rather than keep it.
	C.kt_menu_bar_clear()
	for i, m := range menus {
		ct := C.CString(titles[i])
		C.kt_menu_bar_add(C.uintptr_t(m), ct, C.long(i))
		C.free(unsafe.Pointer(ct))
	}
}
