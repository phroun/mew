# macOS packaging & distribution

How to build the mew graphical app (`mew-sdl`) into a `.app` bundle, make it a
**universal** binary (Intel + Apple Silicon), and sign / notarize it for
distribution to other Macs. All of this is driven by the root `Makefile` targets
and `scripts/macapp.sh`.

> TL;DR — local build: `make macapp-universal`. Distribution:
> `make macapp-universal CODESIGN_ID="Developer ID Application: … (TEAMID)"`
> then `make notarize NOTARY_PROFILE=<profile>`.

---

## The pieces

- **`mew-sdl`** is the graphical host: it links **SDL2 via cgo**, so unlike the
  pure-Go `mew` it needs a C toolchain and an SDL2 for each target architecture.
- **`make macapp`** wraps the native single-arch `mew-sdl` in `bin/mew.app` (icon
  + name). This links SDL2 dynamically against **Homebrew's** SDL2, which is
  *single-arch* and *not bundled* — fine for local use, not portable.
- **`make macapp-universal`** builds a fat (arm64 + x86_64) `mew-sdl`, wraps it,
  and **embeds `SDL2.framework`** so the bundle is self-contained and runs on
  both architectures with no Homebrew dependency.

---

## Universal build

```
make macapp-universal
```

This:

1. Builds `mew-sdl` for `arm64` and `amd64` (`GOARCH`), each with
   `CC="clang -arch <arch>"`, and `lipo`s them into one fat `bin/mew-sdl`.
2. Ad-hoc codesigns the fat binary (Apple Silicon refuses to run unsigned code,
   and `lipo` strips the linker's signature).
3. Wraps it in `bin/mew.app` and copies `SDL2.framework` into
   `Contents/Frameworks/`.

`make mew-sdl-universal` does just step 1–2 (the fat binary, no bundle).

### SDL2 for both architectures

The one prerequisite is a **universal `SDL2.framework`** — one framework
containing both arch slices. The easiest source is **libsdl.org's macOS `.dmg`**
(`SDL2-2.x.x.dmg`); its framework is universal and uses an `@rpath` install name
(important — see troubleshooting). Drop it in `~/Library/Frameworks` (the
default) or point `MACAPP_SDL2_FW` at wherever it lives:

```
make macapp-universal MACAPP_SDL2_FW=/path/to/dir-containing-SDL2.framework
```

Why not Homebrew: `brew install sdl2` gives a *single-arch* dylib, so it can't
satisfy the other slice at build or run time. The universal framework solves
both, and embedding it makes the `.app` portable.

### How the linking is wired (Makefile)

Both slices link against the framework, **not** Homebrew. go-sdl2 hardcodes a
darwin `#cgo pkg-config: sdl2` directive, so the build neutralizes pkg-config
(`PKG_CONFIG=/usr/bin/true`) and supplies SDL2 via explicit cgo flags instead:

```
CGO_CFLAGS  = -F$(MACAPP_SDL2_FW) -I$(MACAPP_SDL2_FW)/SDL2.framework/Headers
CGO_LDFLAGS = -F$(MACAPP_SDL2_FW) -framework SDL2 \
              -Wl,-rpath,@executable_path/../Frameworks
```

The `@executable_path/../Frameworks` rpath is why the binary finds the embedded
copy at runtime: from `Contents/MacOS/mew`, `../Frameworks` is
`Contents/Frameworks/` where `macapp.sh` puts `SDL2.framework`.

> Note: the loose `bin/mew-sdl` will **not** run on its own — that rpath only
> resolves inside the bundle. Run `bin/mew.app`, not `bin/mew-sdl`. (This is why
> `macapp.sh` reads `--version` from the *bundled* copy, not the loose binary.)

### Verify

```
lipo -info bin/mew.app/Contents/MacOS/mew     # -> x86_64 arm64
open bin/mew.app                              # should launch
```

---

## Signing

There are three levels; each is a superset of trust over the last.

### 1. Ad-hoc (default, local only)

With no `CODESIGN_ID`, `macapp.sh` ad-hoc signs (`codesign -s -`). This satisfies
"runs on *this* machine" (Apple Silicon's minimum) but **Gatekeeper blocks it on
any other Mac**.

### 2. Developer ID (for distribution)

```
make macapp-universal CODESIGN_ID="Developer ID Application: Your Name (TEAMID)"
```

`macapp.sh` then signs for distribution:

- **hardened runtime** (`--options runtime`) — required for notarization,
- **secure timestamp** (`--timestamp`) — required for notarization,
- signs **inside-out**: the embedded `SDL2.framework` first, then the app,
- re-verifies with `codesign --verify --deep --strict`.

Because the framework is re-signed with the **same** identity, macOS library
validation passes, so a plain SDL app needs **no entitlements**.

Find your identity string:

```
security find-identity -v -p codesigning
```

### 3. Notarization + stapling (required for a clean first launch elsewhere)

A Developer ID signature **alone** is still Gatekeeper-blocked on download
(quarantine). You must also notarize and staple:

```
make notarize NOTARY_PROFILE=<profile>
```

This zips the app (`ditto`), uploads to Apple's notary service
(`xcrun notarytool submit --wait`, blocks a few minutes), **staples** the ticket
onto the `.app` (`xcrun stapler staple`) so it validates offline, and validates.
Ship the `.app` afterward (zipped or in a `.dmg`).

---

## One-time setup

1. Enroll in the **Apple Developer Program** ($99/yr).
2. Create a **"Developer ID Application"** certificate (Xcode ▸ Settings ▸
   Accounts ▸ Manage Certificates, or the developer portal). It lands in your
   login keychain — that string is your `CODESIGN_ID`.
3. Make an **app-specific password** at appleid.apple.com and store a notary
   credential profile once:

   ```
   xcrun notarytool store-credentials mew-notary \
     --apple-id you@example.com --team-id TEAMID --password <app-specific-pw>
   ```

   Then use `NOTARY_PROFILE=mew-notary`.

---

## Full release sequence

```
make macapp-universal CODESIGN_ID="Developer ID Application: Your Name (TEAMID)"
make notarize NOTARY_PROFILE=mew-notary
# distribute bin/mew.app (zip it or wrap in a dmg)
```

---

## Troubleshooting

- **`make macapp-universal` aborts with `Error 134` (SIGABRT).** dyld couldn't
  load SDL2. If it happens during the build's version read, it's already handled
  (reads the bundled copy). If the finished app aborts on launch with
  *"Library not loaded: @rpath/SDL2.framework…"*, your framework's **install
  name is not `@rpath`-based** (a Homebrew-built framework can be absolute).
  Either use libsdl.org's framework, or fix it after embedding:

  ```
  install_name_tool -id @rpath/SDL2.framework/Versions/A/SDL2 \
    bin/mew.app/Contents/Frameworks/SDL2.framework/Versions/A/SDL2
  # then re-sign the bundle
  ```

- **Notarization rejected.** Get the exact failing item:

  ```
  xcrun notarytool log <submission-id> --keychain-profile mew-notary
  ```

  Usual causes: a nested binary missing hardened runtime or a timestamp — the
  framework-then-app signing above covers the common cases.

- **App Translocation / quarantine.** A downloaded, quarantined `.app` run from
  `~/Downloads` may execute from a read-only translocated path (so
  `os.Executable()` reports that path). Notarized + stapled apps launch normally.
  For the in-app self-installer (copy-to-Applications), a quarantined source can
  carry `com.apple.quarantine` onto the installed copy — strip with
  `xattr -dr com.apple.quarantine <app>` if needed.

- **Duplicate `-rpath` linker warnings** during the build are harmless: go-sdl2's
  darwin cgo already adds `@executable_path/../Frameworks`, so the Makefile's copy
  is a redundant second one the linker drops.

---

## Related

- **Windows packaging** (static cross-build + Start Menu installer) lives in the
  `windows-sdl` / `install-windows.ps1` Makefile targets — see the Makefile
  header and `scripts/install-windows.ps1`.
- The in-app first-run **self-installer** (Windows Start Menu / macOS
  Applications) is `app/internal/selfinstall`.
