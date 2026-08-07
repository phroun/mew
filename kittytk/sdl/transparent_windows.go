//go:build sdl && windows

package sdl

import (
	"unsafe"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
	"golang.org/x/sys/windows"
)

// platformPerPixelAlpha: Windows has no per-pixel window alpha through this
// path; rounded borderless surfaces use SDL shaped windows (SetShape), like
// X11. The OS drop shadow is added separately via DWM (setWindowShadow).
const platformPerPixelAlpha = false

// makeWindowTransparent is the Windows stub: the shaped-window path (SetShape
// on a window created WINDOW_TRANSPARENT) carries the rounding, so there is no
// separate per-pixel arrangement to make here.
func makeWindowTransparent(*sdl3.Window) bool { return false }

// makeWindowMiniaturizable is the Windows stub: SDL's plain Minimize works.
func makeWindowMiniaturizable(*sdl3.Window) {}

var (
	dwmapi                           = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
)

// margins mirrors the Win32 MARGINS struct passed to DwmExtendFrameIntoClientArea.
type margins struct {
	left, right, top, bottom int32
}

// setWindowShadow turns the DWM drop shadow on (or off) for a borderless
// window by extending the desktop-window-manager frame a hair into the client
// area. DWM then draws its standard drop shadow around the window's region —
// which mew has already shaped with SetShape — and because the window server
// owns that shadow it is click-through: a click over the shadow reaches
// whatever window sits beneath, never this one. Turning it off retracts the
// frame to nothing.
//
// This is the low-risk form of the effect: it changes no window styles and
// installs no window-procedure hook, so it cannot disturb SDL's own event
// handling. If a borderless window ever needs the shadow without the faint
// 1px frame line the extension can leave, the follow-up is a WM_NCCALCSIZE
// subclass — deliberately avoided here.
func setWindowShadow(win *sdl3.Window, on bool) {
	if win == nil {
		return
	}
	hwnd := win.Win32HWND()
	if hwnd == 0 {
		return
	}
	m := margins{}
	if on {
		m = margins{1, 1, 1, 1}
	}
	// Ignore the HRESULT: a failure just means no shadow, never a broken window.
	procDwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&m)))
}
