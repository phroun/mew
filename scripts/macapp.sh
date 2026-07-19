#!/usr/bin/env bash
#
# macapp.sh — wrap the graphical mew binary in a macOS .app bundle so it carries
# a proper application name and a Dock / task-switcher icon (which a bare binary
# cannot).
#
#   scripts/macapp.sh <mew-sdl-binary> <assets-dir> <out-dir>
#
# Icon: uses <assets-dir>/mew.icns if present; otherwise, on macOS, builds one
# from <assets-dir>/mew.png (sips + iconutil). With neither, the bundle is built
# without an icon (macOS shows the generic app icon) and a warning is printed.
set -euo pipefail

bin="${1:?usage: macapp.sh <binary> <assets-dir> <out-dir>}"
assets="${2:?usage: macapp.sh <binary> <assets-dir> <out-dir>}"
outdir="${3:?usage: macapp.sh <binary> <assets-dir> <out-dir>}"

name="mew"
bundleid="com.phroun.mew"

[ -f "$bin" ] || { echo "macapp: binary not found: $bin" >&2; exit 1; }

icns="$assets/$name.icns"
png="$assets/$name.png"

# Build the .icns from the PNG only if we don't already have one (macOS only).
if [ ! -f "$icns" ] && [ -f "$png" ]; then
	if command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then
		iconset="$(mktemp -d)/$name.iconset"
		mkdir -p "$iconset"
		for s in 16 32 128 256 512; do
			sips -z "$s" "$s"             "$png" --out "$iconset/icon_${s}x${s}.png"     >/dev/null
			sips -z "$((s*2))" "$((s*2))" "$png" --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
		done
		iconutil -c icns "$iconset" -o "$icns"
		echo "macapp: generated $icns from $png"
	else
		echo "macapp: $png present, but need macOS 'sips' + 'iconutil' to build $icns" >&2
	fi
fi

app="$outdir/$name.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin" "$app/Contents/MacOS/$name"
chmod +x "$app/Contents/MacOS/$name"

# Embed a universal SDL2.framework when provided (MACAPP_SDL2_FW), so the bundle
# carries its own SDL2 and needs no Homebrew install at runtime. A universal
# mew-sdl (make mew-sdl-universal) is linked with an @executable_path/../Frameworks
# rpath, so this embedded copy is what it loads.
if [ -n "${MACAPP_SDL2_FW:-}" ] && [ -d "$MACAPP_SDL2_FW/SDL2.framework" ]; then
	mkdir -p "$app/Contents/Frameworks"
	cp -R "$MACAPP_SDL2_FW/SDL2.framework" "$app/Contents/Frameworks/"
	echo "macapp: embedded SDL2.framework from $MACAPP_SDL2_FW"
fi

icontag=""
if [ -f "$icns" ]; then
	cp "$icns" "$app/Contents/Resources/$name.icns"
	icontag="<key>CFBundleIconFile</key><string>$name</string>"
else
	echo "macapp: no icon at $icns (add assets/mew.icns or assets/mew.png); using the default icon" >&2
fi

# Best-effort version from the binary's --version line ("mew 0.3.1 (…)").
ver="$("$bin" --version 2>/dev/null | awk '{print $2; exit}')"
[ -n "$ver" ] || ver="0"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>$name</string>
	<key>CFBundleDisplayName</key><string>$name</string>
	<key>CFBundleExecutable</key><string>$name</string>
	<key>CFBundleIdentifier</key><string>$bundleid</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundleShortVersionString</key><string>$ver</string>
	<key>CFBundleVersion</key><string>$ver</string>
	<key>NSHighResolutionCapable</key><true/>
	$icontag
</dict>
</plist>
PLIST

# Ad-hoc sign the bundle so it runs locally — Apple Silicon refuses to launch
# unsigned code, and lipo/embedding invalidate any signature the linker applied.
# Distributing to OTHER Macs needs a Developer ID signature + notarization; the
# ad-hoc signature only satisfies "runs on this machine". Best-effort.
if command -v codesign >/dev/null 2>&1; then
	if codesign --force --deep --sign - "$app" >/dev/null 2>&1; then
		echo "macapp: ad-hoc signed $app"
	else
		echo "macapp: codesign failed; the bundle is unsigned (may not launch on Apple Silicon)" >&2
	fi
fi

echo "macapp: built $app"
