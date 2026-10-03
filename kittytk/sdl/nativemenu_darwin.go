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

static id kt_str(const char *s) {
	return ((id (*)(id, SEL, const char *))objc_msgSend)(
		(id)objc_getClass("NSString"), sel_registerName("stringWithUTF8String:"), s);
}

static void kt_item_chosen_imp(id self, SEL _cmd, id sender) {
	long tag = ((long (*)(id, SEL))objc_msgSend)(sender, sel_registerName("tag"));
	kittytkMenuItemChosen(tag);
}

// validateMenuItem: is asked before an item is shown, and says whether it is
// dimmed.
static signed char kt_item_valid_imp(id self, SEL _cmd, id item) {
	long tag = ((long (*)(id, SEL))objc_msgSend)(item, sel_registerName("tag"));
	return kittytkMenuItemValid(tag) ? 1 : 0;
}

// menuNeedsUpdate: is asked before a menu is shown: the moment to write its
// items from the desktop's own.
static void kt_menu_needs_update_imp(id self, SEL _cmd, id menu) {
	kittytkMenuNeedsUpdate((uintptr_t)menu);
}

static int kt_is_windows_menu(id menu);
static void kt_windows_menu_sweep(id menu);

// menuHasKeyEquivalent:forEvent:target:action: is asked in place of AppKit
// searching the menu for a key. The answer is always no: SDL has already
// sent the key on to the desktop, which acts on it through its own keymap,
// as it does on a platform with no menu bar of its own -- so a key the menu
// shows is acted on once, and the way it always is. Asked, AppKit neither
// searches the menu nor brings it up to date.
static signed char kt_menu_has_key_imp(id self, SEL _cmd, id menu, id event, id *target, SEL *action) {
	return 0;
}

// menuWillOpen: comes after menuNeedsUpdate:, and after AppKit has added
// whatever it adds to a Window menu on its way to being shown.
static void kt_menu_will_open_imp(id self, SEL _cmd, id menu) {
	if (kt_is_windows_menu(menu)) kt_windows_menu_sweep(menu);
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
	class_addMethod(cls, sel_registerName("menuWillOpen:"), (IMP)kt_menu_will_open_imp, "v@:@");
	class_addMethod(cls, sel_registerName("menuHasKeyEquivalent:forEvent:target:action:"), (IMP)kt_menu_has_key_imp, "c@:@@^@^:");
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

// Every item of ours carries this marker as its represented object, so the
// items the OS puts in a menu of ours can be told from ours: in the Window
// menu, where AppKit adds its window list and its tiling commands, they are
// swept out. Ours stand together at the top.
static id kt_marker(void) {
	static id m = 0;
	if (!m) {
		m = kt_str("kittytk");
		((id (*)(id, SEL))objc_msgSend)(m, sel_registerName("retain"));
	}
	return m;
}

static int kt_is_ours(id item) {
	id rep = ((id (*)(id, SEL))objc_msgSend)(item, sel_registerName("representedObject"));
	return rep == kt_marker();
}

// kt_insert_ours marks item as ours and puts it after the items of ours
// already at the top of menu.
static void kt_insert_ours(uintptr_t menu, id item) {
	((void (*)(id, SEL, id))objc_msgSend)(item, sel_registerName("setRepresentedObject:"), kt_marker());
	long n = ((long (*)(id, SEL))objc_msgSend)((id)menu, sel_registerName("numberOfItems"));
	long at = 0;
	while (at < n) {
		id it = ((id (*)(id, SEL, long))objc_msgSend)((id)menu, sel_registerName("itemAtIndex:"), at);
		if (!kt_is_ours(it)) break;
		at++;
	}
	((void (*)(id, SEL, id, long))objc_msgSend)((id)menu, sel_registerName("insertItem:atIndex:"), item, at);
}

static int kt_is_windows_menu(id menu) {
	id app = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (!app || !menu) return 0;
	return ((id (*)(id, SEL))objc_msgSend)(app, sel_registerName("windowsMenu")) == menu;
}

// kt_windows_menu_sweep takes out every item AppKit added to the Window
// menu. Its list names our surfaces rather than the desktop's windows, which
// ours lists already, and its tiling commands have no window of ours they
// could act on.
static void kt_windows_menu_sweep(id menu) {
	long n = ((long (*)(id, SEL))objc_msgSend)(menu, sel_registerName("numberOfItems"));
	for (long i = n - 1; i >= 0; i--) {
		id it = ((id (*)(id, SEL, long))objc_msgSend)(menu, sel_registerName("itemAtIndex:"), i);
		if (!kt_is_ours(it)) {
			((void (*)(id, SEL, long))objc_msgSend)(menu, sel_registerName("removeItemAtIndex:"), i);
		}
	}
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

// kt_menu_clear takes out the items of ours, leaving any the OS added --
// except in the Window menu, which is left with none of either.
static void kt_menu_clear(uintptr_t menu) {
	if (kt_is_windows_menu((id)menu)) kt_windows_menu_sweep((id)menu);
	long n = ((long (*)(id, SEL))objc_msgSend)((id)menu, sel_registerName("numberOfItems"));
	for (long i = n - 1; i >= 0; i--) {
		id it = ((id (*)(id, SEL, long))objc_msgSend)((id)menu, sel_registerName("itemAtIndex:"), i);
		if (kt_is_ours(it)) {
			((void (*)(id, SEL, long))objc_msgSend)((id)menu, sel_registerName("removeItemAtIndex:"), i);
		}
	}
}

// kt_menu_add_item appends an item and returns it (the menu holds it). A tag
// above zero sends the item's action, and its validation, to the shared
// target. equiv and mods are the key equivalent it shows; an empty equiv shows none.
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
	kt_insert_ours(menu, item);
	((void (*)(id, SEL))objc_msgSend)(item, sel_registerName("release"));
	return (uintptr_t)item;
}

static void kt_menu_add_separator(uintptr_t menu) {
	id sep = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("separatorItem"));
	kt_insert_ours(menu, sep);
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

// kt_menu_bar_clear takes out everything after the application menu: the
// menus put there last time, and the ones SDL put there to begin with (its
// Window menu, with keys of its own), since no key is to be taken by the
// bar before the desktop's keymap sees it.
static void kt_menu_bar_clear(void) {
	id main = kt_main_menu();
	if (!main) return;
	long n = ((long (*)(id, SEL))objc_msgSend)(main, sel_registerName("numberOfItems"));
	for (long i = n - 1; i >= 1; i--) {
		((void (*)(id, SEL, long))objc_msgSend)(main, sel_registerName("removeItemAtIndex:"), i);
	}
}

// kt_app_menu is the application menu -- the first in the bar, which the OS
// titles with the process's name -- given our delegate so its items are
// written from the desktop's whenever AppKit asks. 0 while there is no bar.
static uintptr_t kt_app_menu(void) {
	id main = kt_main_menu();
	if (!main) return 0;
	long n = ((long (*)(id, SEL))objc_msgSend)(main, sel_registerName("numberOfItems"));
	if (n < 1) return 0;
	id first = ((id (*)(id, SEL, long))objc_msgSend)(main, sel_registerName("itemAtIndex:"), 0);
	id menu = ((id (*)(id, SEL))objc_msgSend)(first, sel_registerName("submenu"));
	if (!menu) return 0;
	// From empty: what SDL put there first (its About, Hide and Quit, with
	// keys of their own) goes, and only ours are written back.
	((void (*)(id, SEL))objc_msgSend)(menu, sel_registerName("removeAllItems"));
	((void (*)(id, SEL, id))objc_msgSend)(menu, sel_registerName("setDelegate:"), kt_menu_delegate());
	return (uintptr_t)menu;
}

// kt_menu_add_services appends a Services item holding the OS's own Services
// menu, when the application has one.
static void kt_menu_add_services(uintptr_t menu) {
	id app = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (!app) return;
	id services = ((id (*)(id, SEL))objc_msgSend)(app, sel_registerName("servicesMenu"));
	if (!services) return;
	id item = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSMenuItem"), sel_registerName("alloc"));
	item = ((id (*)(id, SEL, id, SEL, id))objc_msgSend)(item,
		sel_registerName("initWithTitle:action:keyEquivalent:"), kt_str("Services"), (SEL)0, kt_str(""));
	((void (*)(id, SEL, id))objc_msgSend)(item, sel_registerName("setSubmenu:"), services);
	kt_insert_ours(menu, item);
	((void (*)(id, SEL))objc_msgSend)(item, sel_registerName("release"));
}

// kt_set_windows_menu tells AppKit which menu is the Window menu (0 for
// none). AppKit adds items of its own there, which the sweep takes out again:
// now, and each time the menu is brought up to date or opened.
static void kt_set_windows_menu(uintptr_t menu) {
	id app = ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (!app) return;
	((void (*)(id, SEL, id))objc_msgSend)(app, sel_registerName("setWindowsMenu:"), (id)menu);
	if (menu) kt_windows_menu_sweep((id)menu);
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
	app := nativeMenus.app
	menus := nativeMenus.set
	have := nativeMenus.haveSet
	nativeMenus.mu.Unlock()
	if !have {
		return
	}
	pool := C.kt_pool_push()
	defer C.kt_pool_pop(pool)
	setNativeBar(cocoaMenuSink{}, app, menus)
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

func (cocoaMenuSink) appMenu() uintptr { return uintptr(C.kt_app_menu()) }

func (cocoaMenuSink) addServices(menu uintptr) { C.kt_menu_add_services(C.uintptr_t(menu)) }

func (cocoaMenuSink) setWindowsMenu(menu uintptr) { C.kt_set_windows_menu(C.uintptr_t(menu)) }

func (cocoaMenuSink) setBar(menus []uintptr, titles []string) {
	// With no bar, kt_menu_bar_add lets each menu go rather than keep it.
	C.kt_menu_bar_clear()
	for i, m := range menus {
		ct := C.CString(titles[i])
		C.kt_menu_bar_add(C.uintptr_t(m), ct, C.long(i))
		C.free(unsafe.Pointer(ct))
	}
}
