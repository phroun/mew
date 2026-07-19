//go:build windows

// Package wininstall is mew's own Windows installer — no external installer
// framework, no PowerShell, just plain Go hitting the same Win32/COM APIs a real
// installer uses. It copies the two binaries into a per-user location, drops a
// Start Menu shortcut pointing at the graphical build (mew-sdl.exe, which carries
// the embedded app icon), adds the install directory to the user's PATH, and
// records a first-run flag in the registry. Everything lands under HKCU and
// %LOCALAPPDATA%/%APPDATA%, so no elevation is needed.
//
// Both the console binary's `mew --install` flag and the graphical binary's
// first-run welcome window (Install button) call Install(); it is idempotent.
package wininstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const installAppName = "mew"

// COINIT_APARTMENTTHREADED — declared locally so we don't depend on the constant
// being exported by x/sys/windows across versions.
const coinitApartmentThreaded = 0x2

var (
	modole32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modole32.NewProc("CoCreateInstance")

	moduser32              = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = moduser32.NewProc("SendMessageTimeoutW")
)

// Available reports that the installer is usable on this platform (it is — this
// is the Windows build).
func Available() bool { return true }

// FirstRunDone reports whether mew has been installed on this machine (the
// HKCU\Software\mew "Installed" flag is set). The graphical host shows its
// first-run welcome window only when this is false.
func FirstRunDone() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\`+installAppName, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("Installed")
	return err == nil && v != 0
}

// Install copies the binaries into place, creates the Start Menu shortcut, adds
// the install directory to PATH, and sets the first-run flag. It returns the
// path of the installed graphical binary (mew-sdl.exe), which the caller can
// relaunch; on a console-only install it returns the console binary instead.
func Install() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	srcDir := filepath.Dir(self)
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// Copy the console binary and the graphical binary beside it. mew-sdl.exe may
	// be absent if only `make windows` was run (the GUI build needs a Windows cgo
	// toolchain) — warn and carry on with just the console binary.
	var installedSDL, installedConsole string
	for _, name := range []string{"mew.exe", "mew-sdl.exe"} {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err != nil {
			if name == "mew.exe" {
				src = self // we might be invoked under a different name
			} else {
				fmt.Fprintf(os.Stderr, "mew: %s not found beside me; skipping (build it with `make windows-sdl` on Windows)\n", name)
				continue
			}
		}
		dst := filepath.Join(dir, name)
		if err := copyFile(src, dst); err != nil {
			return "", fmt.Errorf("copy %s: %w", name, err)
		}
		switch name {
		case "mew-sdl.exe":
			installedSDL = dst
		case "mew.exe":
			installedConsole = dst
		}
		fmt.Printf("installed %s\n", dst)
	}

	// Start Menu shortcut → the graphical binary (embedded icon); fall back to
	// the console binary if the graphical one wasn't available.
	guiExe := installedSDL
	if guiExe == "" {
		guiExe = installedConsole
	}
	if guiExe == "" {
		return "", fmt.Errorf("no binary was installed, nothing to link")
	}
	lnk := shortcutPath()
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return "", err
	}
	if err := createShortcut(lnk, guiExe, guiExe, dir, "mew editor"); err != nil {
		return "", fmt.Errorf("create Start Menu shortcut: %w", err)
	}
	fmt.Printf("created Start Menu shortcut: %s\n", lnk)

	if added, err := addToUserPath(dir); err != nil {
		return "", fmt.Errorf("update PATH: %w", err)
	} else if added {
		fmt.Printf("added %s to your PATH (open a new console to pick it up)\n", dir)
	} else {
		fmt.Printf("%s already on your PATH\n", dir)
	}

	if err := markInstalled(); err != nil {
		return "", fmt.Errorf("record install: %w", err)
	}
	return guiExe, nil
}

// Uninstall reverses Install (best-effort): removes the Start Menu shortcut, the
// PATH entry, the installed binaries, and the first-run flag.
func Uninstall() error {
	dir := installDir()
	lnk := shortcutPath()

	if err := os.Remove(lnk); err == nil {
		fmt.Printf("removed %s\n", lnk)
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "mew: could not remove %s: %v\n", lnk, err)
	}

	if removed, err := removeFromUserPath(dir); err != nil {
		fmt.Fprintf(os.Stderr, "mew: could not update PATH: %v\n", err)
	} else if removed {
		fmt.Printf("removed %s from your PATH\n", dir)
	}

	// Windows won't delete a running .exe, so if the user ran the installed copy
	// its own file survives — best-effort.
	for _, name := range []string{"mew.exe", "mew-sdl.exe"} {
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			fmt.Printf("removed %s\n", filepath.Join(dir, name))
		}
	}
	os.Remove(dir) // succeeds only if now empty

	if err := registry.DeleteKey(registry.CURRENT_USER, `Software\`+installAppName); err != nil && err != registry.ErrNotExist {
		fmt.Fprintf(os.Stderr, "mew: could not clear registry flag: %v\n", err)
	}
	return nil
}

// installDir is where the binaries land: %LOCALAPPDATA%\Programs\mew (per-user,
// no elevation). shortcutPath is this user's Start Menu .lnk.
func installDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	return filepath.Join(base, "Programs", installAppName)
}

func shortcutPath() string {
	base := os.Getenv("APPDATA")
	return filepath.Join(base, "Microsoft", "Windows", "Start Menu", "Programs", installAppName+".lnk")
}

// markInstalled records the first-run flag (HKCU\Software\mew "Installed"=1) so
// the graphical host stops showing its welcome window.
func markInstalled() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\`+installAppName, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue("Installed", 1)
}

func copyFile(src, dst string) error {
	if strings.EqualFold(src, dst) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	clErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return clErr
}

// createShortcut writes a Windows .lnk via the ShellLink COM object (the same
// thing WScript.Shell.CreateShortcut drives, one layer down). We build the vtable
// call sites by hand: CoCreateInstance gives an IShellLinkW, we set its target /
// working dir / description / icon, then QueryInterface to IPersistFile and Save.
func createShortcut(lnkPath, target, iconPath, workDir, desc string) error {
	// Best-effort COM init; S_FALSE/RPC_E_CHANGED_MODE are harmless for a
	// one-shot process, and we uninitialize on the way out regardless.
	_ = windows.CoInitializeEx(0, coinitApartmentThreaded)
	defer windows.CoUninitialize()

	clsidShellLink, err := windows.GUIDFromString("{00021401-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}
	iidShellLinkW, err := windows.GUIDFromString("{000214F9-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}
	iidPersistFile, err := windows.GUIDFromString("{0000010B-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}

	var psl *comShellLink
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		uintptr(windows.CLSCTX_INPROC_SERVER),
		uintptr(unsafe.Pointer(&iidShellLinkW)),
		uintptr(unsafe.Pointer(&psl)),
	)
	if int32(r) < 0 || psl == nil {
		return fmt.Errorf("CoCreateInstance(ShellLink): 0x%08x", uint32(r))
	}
	defer comRelease(psl.vtbl.Release, unsafe.Pointer(psl))

	if err := comSetStr(psl.vtbl.SetPath, unsafe.Pointer(psl), target); err != nil {
		return fmt.Errorf("SetPath: %w", err)
	}
	if workDir != "" {
		_ = comSetStr(psl.vtbl.SetWorkingDirectory, unsafe.Pointer(psl), workDir)
	}
	if desc != "" {
		_ = comSetStr(psl.vtbl.SetDescription, unsafe.Pointer(psl), desc)
	}
	if iconPath != "" {
		if ip, err := windows.UTF16PtrFromString(iconPath); err == nil {
			syscall.SyscallN(psl.vtbl.SetIconLocation, uintptr(unsafe.Pointer(psl)), uintptr(unsafe.Pointer(ip)), 0)
		}
	}

	var ppf *comPersistFile
	r, _, _ = syscall.SyscallN(psl.vtbl.QueryInterface,
		uintptr(unsafe.Pointer(psl)),
		uintptr(unsafe.Pointer(&iidPersistFile)),
		uintptr(unsafe.Pointer(&ppf)))
	if int32(r) < 0 || ppf == nil {
		return fmt.Errorf("QueryInterface(IPersistFile): 0x%08x", uint32(r))
	}
	defer comRelease(ppf.vtbl.Release, unsafe.Pointer(ppf))

	lnkPtr, err := windows.UTF16PtrFromString(lnkPath)
	if err != nil {
		return err
	}
	r, _, _ = syscall.SyscallN(ppf.vtbl.Save,
		uintptr(unsafe.Pointer(ppf)),
		uintptr(unsafe.Pointer(lnkPtr)),
		1) // fRemember = TRUE
	if int32(r) < 0 {
		return fmt.Errorf("IPersistFile.Save: 0x%08x", uint32(r))
	}
	return nil
}

// comShellLink / comPersistFile mirror the COM object layout: the first word is
// a pointer to the method-table (vtbl), whose fields are the method addresses in
// interface order. We only name the slots up to the ones we call.
type comShellLink struct{ vtbl *comVtblShellLink }

type comVtblShellLink struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type comPersistFile struct{ vtbl *comVtblPersistFile }

type comVtblPersistFile struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetClassID     uintptr
	IsDirty        uintptr
	Load           uintptr
	Save           uintptr
	SaveCompleted  uintptr
	GetCurFile     uintptr
}

// comSetStr calls a COM method whose only argument is a wide-string pointer
// (SetPath, SetWorkingDirectory, SetDescription).
func comSetStr(method uintptr, this unsafe.Pointer, s string) error {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return err
	}
	r, _, _ := syscall.SyscallN(method, uintptr(this), uintptr(unsafe.Pointer(p)))
	if int32(r) < 0 {
		return fmt.Errorf("0x%08x", uint32(r))
	}
	return nil
}

func comRelease(method uintptr, this unsafe.Pointer) {
	syscall.SyscallN(method, uintptr(this))
}

// addToUserPath appends dir to the user's PATH (HKCU\Environment) if it isn't
// already there, preserving the value's type (PATH is conventionally EXPAND_SZ),
// then broadcasts the change so new processes see it without a re-login.
func addToUserPath(dir string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()

	cur, valType, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return false, err
	}
	for _, p := range strings.Split(cur, ";") {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			return false, nil
		}
	}
	next := dir
	if cur != "" {
		next = strings.TrimRight(cur, ";") + ";" + dir
	}
	if valType == registry.SZ {
		err = k.SetStringValue("Path", next)
	} else {
		err = k.SetExpandStringValue("Path", next) // EXPAND_SZ or a fresh value
	}
	if err != nil {
		return false, err
	}
	broadcastEnvChange()
	return true, nil
}

func removeFromUserPath(dir string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()

	cur, valType, err := k.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var kept []string
	removed := false
	for _, p := range strings.Split(cur, ";") {
		if p == "" {
			continue
		}
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	if !removed {
		return false, nil
	}
	next := strings.Join(kept, ";")
	if valType == registry.SZ {
		err = k.SetStringValue("Path", next)
	} else {
		err = k.SetExpandStringValue("Path", next)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvChange()
	return true, nil
}

// broadcastEnvChange tells running processes (Explorer, shells) that the
// environment changed, so freshly launched programs inherit the new PATH.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}
