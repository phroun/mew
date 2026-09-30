//go:build sdl

package sdl

import (
	"sync"
	"unicode/utf8"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// The native menu bar's portable half (platform.NativeMenuBarHost). The
// desktop hands over the menus its own bar shows; each is written into a
// native menu, and every item that does something is given a tag naming its
// entry -- the action to run and the question of whether it may run now.
// Choosing the item, or pressing its key, comes back here with the tag.
//
// A menu's items are written when the bar is set and written again each time
// the OS asks (about to show the menu, or looking through it for a key), from
// the desktop's own menus at that moment, so the native bar is never staler
// than the one the desktop draws.
//
// Only macOS has such a bar, so only nativemenu_darwin.go writes a real one;
// the writing is here, against a sink, so it can be tested anywhere.

type nativeEntry struct {
	action  func()
	enabled func() bool
}

var nativeMenus struct {
	mu       sync.Mutex
	app      platform.NativeMenu   // the latest application menu, applied once the bar exists
	set      []platform.NativeMenu // and the menus after it
	haveSet  bool
	appMenu  uintptr // the OS's application menu, while this bar fills it
	post     func(func())
	models   map[uintptr]platform.NativeMenu // each native menu's source
	tags     map[uintptr][]int               // the tags each native menu's items hold
	children map[uintptr][]uintptr           // the submenus each native menu holds
	entries  map[int]nativeEntry
	nextTag  int
}

// SetNativeMenus implements platform.NativeMenuBarHost.
func (p *Platform) SetNativeMenus(app platform.NativeMenu, menus []platform.NativeMenu) {
	nativeMenus.mu.Lock()
	nativeMenus.app = app
	nativeMenus.set = menus
	nativeMenus.haveSet = true
	nativeMenus.post = p.Post
	nativeMenus.mu.Unlock()
	// The bar can be written once the application is up; before that, Run
	// writes it when it gets there.
	if p.main != nil {
		applyNativeMenus()
	}
}

// nativeMenuSink is what the bar is written into: AppKit on macOS, a
// recorder in the tests. Menus and items are handles the sink hands out.
type nativeMenuSink interface {
	newMenu(title string) uintptr
	clear(menu uintptr)
	addItem(menu uintptr, title string, tag int, checked bool, equiv string, mods uint) uintptr
	addSeparator(menu uintptr)
	setSubmenu(item, submenu uintptr)
	// appMenu is the OS's application menu, for the bar to fill, or 0 when
	// there is none yet.
	appMenu() uintptr
	// addServices appends the OS's Services submenu, where it has one.
	addServices(menu uintptr)
	// setBar puts menus in the OS menu bar after the application menu, in
	// place of everything that followed it before.
	setBar(menus []uintptr, titles []string)
	// setWindowsMenu tells the OS which menu is the Window menu, or that
	// none is, for 0.
	setWindowsMenu(menu uintptr)
}

// setNativeBar writes the application menu and menus into s from nothing and
// puts them in the bar.
func setNativeBar(s nativeMenuSink, app platform.NativeMenu, menus []platform.NativeMenu) {
	nativeMenus.mu.Lock()
	nativeMenus.models = map[uintptr]platform.NativeMenu{}
	nativeMenus.tags = map[uintptr][]int{}
	nativeMenus.children = map[uintptr][]uintptr{}
	nativeMenus.entries = map[int]nativeEntry{}
	nativeMenus.appMenu = 0
	nativeMenus.mu.Unlock()

	if h := s.appMenu(); h != 0 {
		nativeMenus.mu.Lock()
		nativeMenus.models[h] = app
		nativeMenus.appMenu = h
		nativeMenus.mu.Unlock()
		fillNativeMenu(s, h)
	}

	handles := make([]uintptr, 0, len(menus))
	titles := make([]string, 0, len(menus))
	var windows uintptr
	for _, m := range menus {
		h := s.newMenu(m.Title)
		nativeMenus.mu.Lock()
		nativeMenus.models[h] = m
		nativeMenus.mu.Unlock()
		fillNativeMenu(s, h)
		handles = append(handles, h)
		titles = append(titles, m.Title)
		if m.Windows && windows == 0 {
			windows = h
		}
	}
	s.setBar(handles, titles)
	// Named every time, none included: the OS must not go on listing windows
	// in a menu that has left the bar.
	s.setWindowsMenu(windows)
}

// fillNativeMenu writes a native menu's items afresh from its source, letting
// go of the tags and submenus its last items held. A handle not written by
// this bar is left alone.
func fillNativeMenu(s nativeMenuSink, menu uintptr) {
	nativeMenus.mu.Lock()
	model, ok := nativeMenus.models[menu]
	if ok {
		forgetNativeContentsLocked(menu)
	}
	isApp := menu == nativeMenus.appMenu
	nativeMenus.mu.Unlock()
	if !ok {
		return
	}
	var items []platform.NativeMenuItem
	if model.Items != nil {
		items = model.Items()
	}
	// The application menu carries the OS's Services too, where people look
	// for them: in a group of their own before the last group, the one that
	// ends the session -- or at the end, when the menu is all one group.
	services := -1
	if isApp {
		services = len(items)
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Separator {
				services = i
				break
			}
		}
	}
	s.clear(menu)
	for i, it := range items {
		if i == services {
			s.addSeparator(menu)
			s.addServices(menu)
		}
		if it.Separator {
			s.addSeparator(menu)
			continue
		}
		if it.Submenu != nil {
			item := s.addItem(menu, it.Title, 0, it.Checked, "", 0)
			sub := s.newMenu(it.Title)
			nativeMenus.mu.Lock()
			nativeMenus.models[sub] = *it.Submenu
			nativeMenus.children[menu] = append(nativeMenus.children[menu], sub)
			nativeMenus.mu.Unlock()
			fillNativeMenu(s, sub)
			s.setSubmenu(item, sub)
			continue
		}
		nativeMenus.mu.Lock()
		nativeMenus.nextTag++
		tag := nativeMenus.nextTag
		nativeMenus.entries[tag] = nativeEntry{action: it.Action, enabled: it.Enabled}
		nativeMenus.tags[menu] = append(nativeMenus.tags[menu], tag)
		nativeMenus.mu.Unlock()
		equiv, mods, _ := macKeyEquivalent(it.Key)
		s.addItem(menu, it.Title, tag, it.Checked, equiv, mods)
	}
	if services == len(items) {
		if len(items) > 0 {
			s.addSeparator(menu)
		}
		s.addServices(menu)
	}
}

// forgetNativeContentsLocked drops the entries a menu's items held, and its
// submenus with everything they held. nativeMenus.mu held.
func forgetNativeContentsLocked(menu uintptr) {
	for _, tag := range nativeMenus.tags[menu] {
		delete(nativeMenus.entries, tag)
	}
	delete(nativeMenus.tags, menu)
	for _, sub := range nativeMenus.children[menu] {
		forgetNativeContentsLocked(sub)
		delete(nativeMenus.models, sub)
	}
	delete(nativeMenus.children, menu)
}

// nativeItemEnabled answers the OS validating an item: whether the entry its
// tag names may run now. A tag naming nothing -- left from items since
// replaced -- may not.
func nativeItemEnabled(tag int) bool {
	nativeMenus.mu.Lock()
	e, ok := nativeMenus.entries[tag]
	nativeMenus.mu.Unlock()
	if !ok || e.action == nil {
		return false
	}
	return e.enabled == nil || e.enabled()
}

// nativeItemChosen posts the action the chosen item's tag names.
func nativeItemChosen(tag int) {
	nativeMenus.mu.Lock()
	e, ok := nativeMenus.entries[tag]
	post := nativeMenus.post
	nativeMenus.mu.Unlock()
	if !ok || e.action == nil || post == nil {
		return
	}
	post(e.action)
}

// The modifier flags an NSMenuItem's key equivalent takes
// (NSEventModifierFlags).
const (
	macShiftFlag   uint = 1 << 17
	macControlFlag uint = 1 << 18
	macOptionFlag  uint = 1 << 19
	macCommandFlag uint = 1 << 20
)

// macNamedKeys is each named key's key-equivalent character: the ASCII
// control characters AppKit uses, and its private-use function-key codes.
var macNamedKeys = map[string]rune{
	"Return":    '\r',
	"Enter":     0x03,
	"Tab":       '\t',
	"Escape":    0x1b,
	"Space":     ' ',
	"Backspace": 0x08, // the Mac's Delete key, which erases behind
	"Delete":    0x08, // DEL, which KittyTK spells as erasing behind too
	"FDel":      0xF728,
	"Up":        0xF700,
	"Down":      0xF701,
	"Left":      0xF702,
	"Right":     0xF703,
	"Home":      0xF729,
	"End":       0xF72B,
	"PageUp":    0xF72C,
	"PageDown":  0xF72D,
}

// macKeyEquivalent turns a KittyTK key spelling into an NSMenuItem key
// equivalent: the character and the modifier flags. ok is false for a key
// the OS has no way to say -- a chord of several presses, a modifier the Mac
// keyboard does not have -- and for one with no Command, Control or Option
// that is not a function key, which bound in the menu bar would be taken
// from under the person typing.
func macKeyEquivalent(key string) (equiv string, mods uint, ok bool) {
	if key == "" {
		return "", 0, false
	}
	for _, r := range key {
		if r == ' ' {
			return "", 0, false
		}
	}
	km, name := core.KeyParts(key)
	if km&(core.MicroModifier|core.HyperModifier|core.GlyphModifier) != 0 {
		return "", 0, false
	}
	if km&core.SuperModifier != 0 {
		mods |= macCommandFlag
	}
	if km&core.ControlModifier != 0 {
		mods |= macControlFlag
	}
	if km&core.MegaModifier != 0 {
		mods |= macOptionFlag
	}
	if km&core.ShiftModifier != 0 {
		mods |= macShiftFlag
	}

	function := false
	switch {
	case len(name) >= 2 && name[0] == 'F' && isDigits(name[1:]):
		n := atoiSmall(name[1:])
		if n < 1 || n > 35 {
			return "", 0, false
		}
		equiv = string(rune(0xF704 + n - 1))
		function = true
	case utf8.RuneCountInString(name) == 1:
		r, _ := utf8.DecodeRuneInString(name)
		if r >= 'A' && r <= 'Z' {
			// The key is the letter; Shift, where meant, is already a flag.
			r += 'a' - 'A'
		}
		equiv = string(r)
	default:
		r, known := macNamedKeys[name]
		if !known {
			return "", 0, false
		}
		equiv = string(r)
	}
	if mods&(macCommandFlag|macControlFlag|macOptionFlag) == 0 && !function {
		return "", 0, false
	}
	return equiv, mods, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atoiSmall(s string) int {
	n := 0
	for i := 0; i < len(s) && n < 1000; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
