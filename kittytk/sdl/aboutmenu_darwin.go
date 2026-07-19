//go:build sdl && darwin

package sdl

/*
#cgo LDFLAGS: -framework Cocoa
#include <objc/runtime.h>
#include <objc/message.h>

// Defined in Go via //export (see aboutmenu_export_darwin.go).
extern void kittytkAboutClicked(void);

// kittytk_about_imp is the IMP behind the retargeted About menu item; it just
// calls back into Go on the main thread (where AppKit menu actions fire).
static void kittytk_about_imp(id self, SEL _cmd, id sender) {
	kittytkAboutClicked();
}

// kittytk_install_about_handler retargets the application menu's standard
// "About <app>" item (whose action is orderFrontStandardAboutPanel:) to our own
// handler, so it shows mew's About dialog instead of the default Cocoa panel.
// It walks NSApp.mainMenu -> item 0 (the app menu) -> its submenu, finds the
// About item by its standard action (falling back to the first item), and points
// its target/action at a shared KittyTKAboutTarget instance. Idempotent and a
// no-op if the menu isn't built yet.
static void kittytk_install_about_handler(void) {
	Class appCls = objc_getClass("NSApplication");
	if (!appCls) return;
	id app = ((id (*)(Class, SEL))objc_msgSend)(appCls, sel_registerName("sharedApplication"));
	if (!app) return;
	id mainMenu = ((id (*)(id, SEL))objc_msgSend)(app, sel_registerName("mainMenu"));
	if (!mainMenu) return;
	long count = ((long (*)(id, SEL))objc_msgSend)(mainMenu, sel_registerName("numberOfItems"));
	if (count < 1) return;
	id appMenuItem = ((id (*)(id, SEL, long))objc_msgSend)(mainMenu, sel_registerName("itemAtIndex:"), 0);
	if (!appMenuItem) return;
	id appMenu = ((id (*)(id, SEL))objc_msgSend)(appMenuItem, sel_registerName("submenu"));
	if (!appMenu) return;

	SEL aboutSel = sel_registerName("orderFrontStandardAboutPanel:");
	long idx = ((long (*)(id, SEL, id, SEL))objc_msgSend)(
		appMenu, sel_registerName("indexOfItemWithTarget:andAction:"), (id)0, aboutSel);
	id aboutItem = 0;
	if (idx >= 0) {
		aboutItem = ((id (*)(id, SEL, long))objc_msgSend)(appMenu, sel_registerName("itemAtIndex:"), idx);
	} else {
		aboutItem = ((id (*)(id, SEL, long))objc_msgSend)(appMenu, sel_registerName("itemAtIndex:"), 0);
	}
	if (!aboutItem) return;

	// A shared target object whose kittytkAbout: calls kittytk_about_imp. Built
	// once and reused across calls.
	static Class targetCls = NULL;
	static id target = NULL;
	if (targetCls == NULL) {
		targetCls = objc_allocateClassPair((Class)objc_getClass("NSObject"), "KittyTKAboutTarget", 0);
		if (targetCls) {
			class_addMethod(targetCls, sel_registerName("kittytkAbout:"), (IMP)kittytk_about_imp, "v@:@");
			objc_registerClassPair(targetCls);
		}
	}
	if (targetCls != NULL && target == NULL) {
		id t = ((id (*)(id, SEL))objc_msgSend)((id)targetCls, sel_registerName("alloc"));
		target = ((id (*)(id, SEL))objc_msgSend)(t, sel_registerName("init"));
	}
	if (target == NULL) return;

	((void (*)(id, SEL, id))objc_msgSend)(aboutItem, sel_registerName("setTarget:"), target);
	((void (*)(id, SEL, SEL))objc_msgSend)(aboutItem, sel_registerName("setAction:"), sel_registerName("kittytkAbout:"));
}
*/
import "C"

// installAboutMenuHandler points the macOS application menu's About item at our
// Go callback. Safe to call more than once (idempotent) and before the menu
// exists (no-op then).
func installAboutMenuHandler() {
	C.kittytk_install_about_handler()
}
