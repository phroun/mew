# Windows packaging & distribution

How to build the mew binaries for Windows — the console `mew.exe` and the
graphical `mew-sdl.exe` (with icon) — and install them with a Start Menu
shortcut. Driven by the root `Makefile` targets and `scripts/install-windows.ps1`.

> TL;DR — console: `make windows` (cross-builds anywhere). GUI:
> `make windows-sdl` (static cross-build from macOS/Linux by default, or native
> on Windows). Install: `scripts\install-windows.ps1` on Windows.

---

## The two binaries

- **`mew.exe`** — the terminal host. **Pure Go**, so it cross-builds from any OS
  with no cgo. It keeps Go's default console subsystem (it's a console editor).
  Recognizes `--window` / `--detach`, handing off to `mew-sdl.exe` beside it.
- **`mew-sdl.exe`** — the graphical host. Links **SDL2 via cgo**, so it needs a
  Windows C toolchain (mingw-w64) and a Windows SDL2. It carries the **app icon**
  (embedded resource) and is built `-H windowsgui` (no console window).

The icon rides on the **GUI** binary (what shows in Explorer / the taskbar / the
Start Menu), not the console one.

---

## Console `mew.exe`

```
make windows                      # amd64 by default
make windows WINDOWS_ARCH=arm64   # or arm64
```

Pure Go, `CGO_ENABLED=0` — builds from macOS/Linux/Windows alike. No icon (that's
on the GUI binary).

---

## Graphical `mew-sdl.exe`

`mew-sdl.exe` needs a **Windows** C toolchain and a **Windows** SDL2 — the host
gcc and a Linux/macOS SDL2 can't produce it. go-sdl2 links Windows SDL2 with a
plain `-lSDL2` and includes `<SDL2/SDL.h>` (it does **not** use pkg-config on
Windows), so those just need to be on the compiler's search path.

### Default: static cross-build from macOS/Linux

```
make windows-sdl
```

The default is a **cross build that statically links SDL2**, so the result is one
self-contained `mew-sdl.exe` with **no `SDL2.dll` to ship**. Prerequisites:

1. The **mingw-w64** cross toolchain (`x86_64-w64-mingw32-gcc`).
   (macOS: `brew install mingw-w64`. Linux: your distro's `mingw-w64` package.)
2. The **SDL2 mingw dev package** — `SDL2-devel-<ver>-mingw` from libsdl.org.
   Unpack it and point `SDL2_DIR` at it; `SDL2_MINGW` is its `x86_64-w64-mingw32`
   subdir (the one holding `include/SDL2/SDL.h` and `lib/libSDL2.a`).

Defaults (override any):

```
WINDOWS_CC   = x86_64-w64-mingw32-gcc
SDL2_STATIC  = 1
SDL2_VERSION = 2.32.10
SDL2_DIR     = $(HOME)/projects/vendor/SDL2-$(SDL2_VERSION)
SDL2_MINGW   = $(SDL2_DIR)/x86_64-w64-mingw32
```

Example with a different location:

```
make windows-sdl SDL2_MINGW=/abs/path/x86_64-w64-mingw32
```

### The static-link mechanics (why the flags look odd)

go-sdl2 emits a bare `-lSDL2`, and with both `libSDL2.a` and `libSDL2.dll.a`
present the linker prefers the **dynamic** import lib. To force static regardless
of flag order, the Makefile uses **`-static`** — a global link *mode*, not a
positional flag — inside a `--start-group/--end-group` so SDL2 and its Windows
system dependencies resolve in any order, and drops `-lSDL2main` (the Go runtime
owns the entry point). The result is fully static (libgcc/winpthread baked in
too). The Windows system libs it pulls in mirror `sdl2-config --static-libs`.

Set `SDL2_STATIC=` (empty) to link **dynamically** instead — then `mew-sdl.exe`
needs `SDL2.dll` beside it at runtime (from the dev package's `bin/`, or the SDL2
runtime zip).

### Native Windows build

On Windows with MSYS2's mingw-w64 gcc and SDL2 in the sysroot, no cross flags are
needed:

```
make windows-sdl WINDOWS_CC= SDL2_MINGW= SDL2_STATIC=
```

### The icon

The app icon is an embedded COFF resource. `make windows-sdl` regenerates it from
`assets/mew.ico` via `rsrc` into `app/cmd/mew-sdl/rsrc_windows_<arch>.syso`; the
Go linker embeds any `*_windows_<arch>.syso` in that package automatically (the
arch suffix scopes it to the matching build). The `.syso` is git-ignored.

---

## Installing (Start Menu + PATH)

`scripts\install-windows.ps1` installs a built copy on Windows — no external
installer framework, all per-user (no elevation):

```
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

It:

- copies `mew.exe` + `mew-sdl.exe` into `%LOCALAPPDATA%\Programs\mew\` (side by
  side, so `mew.exe`'s `--window`/`--detach` handoff finds the GUI binary),
- creates a **Start Menu shortcut** (`…\Start Menu\Programs\mew.lnk`) pointing at
  the icon-bearing `mew-sdl.exe`,
- adds the install dir to the **user PATH** (`HKCU\Environment`) and broadcasts
  `WM_SETTINGCHANGE` so new consoles pick it up.

Flags: `-AllUsers` (elevated: Program Files + all-users Start Menu + machine
PATH), `-InstallDir <path>`, `-NoPath`, `-Uninstall` (reverses all of it).

Build first — on Windows, or cross-build then copy over:

```
make windows        # mew.exe
make windows-sdl    # mew-sdl.exe (static, self-contained)
```

---

## In-app self-installer

The graphical binary also offers to install itself on first run (the **Welcome**
window's *Install* button), and `mew --install` / `--uninstall` do the same from
the console. On Windows this is the same Start Menu + PATH install as the script
(implemented in Go: `app/internal/selfinstall`, `selfinstall_windows.go`, using
the ShellLink COM object for the `.lnk` and `HKCU\Environment` for PATH).

"Already installed" is judged from the binary's location — a copy whose parent
folder is named exactly `mew` (where the installer puts it,
`%LOCALAPPDATA%\Programs\mew`) is treated as installed and skips the welcome; a
copy run from an extracted download folder (`mew-0.3.2-sdl\`, …) still offers to
install. No registry flag.

---

## Distribution notes

- The static `mew-sdl.exe` is self-contained — ship `mew.exe` + `mew-sdl.exe`
  together (a zip is enough) so the `--window` handoff works.
- **Code signing** (Authenticode) is optional but avoids SmartScreen "unknown
  publisher" warnings on download. It needs a code-signing certificate (an OV or
  EV cert from a CA) and `signtool sign /fd sha256 /tr <timestamp-url> /td sha256
  mew-sdl.exe`. Not wired into the Makefile — do it on Windows after building.
  (EV certs clear SmartScreen immediately; OV certs build reputation over time.)

---

## Troubleshooting

- **`windows.h` not found** — the C compiler is your host gcc, not mingw. Set
  `WINDOWS_CC=x86_64-w64-mingw32-gcc` (and install mingw-w64).
- **`SDL2/SDL.h: No such file or directory`** — `SDL2_MINGW` isn't pointing at the
  mingw dev package's arch subdir (needs `include/SDL2/SDL.h`).
- **Linker can't find `SDL2` / unresolved `SDL_*`** — the static libs aren't where
  `SDL2_MINGW/lib` expects (`libSDL2.a`), or you have only the runtime, not the
  dev package.
- **`mew-sdl.exe` won't start on a clean machine (dynamic build)** — `SDL2.dll`
  isn't beside it. Either ship the DLL or use the default static build.

---

## Related

- **macOS packaging** (universal build + Developer ID signing + notarization) is
  in `docs/dev-notes/macos-packaging.md`.
- The in-app self-installer is `app/internal/selfinstall` (Windows + macOS).
