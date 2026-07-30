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

# Make the binary load SDL2 via @rpath so the EMBEDDED framework is what runs,
# no matter where the build's SDL2 came from. libsdl.org's framework already uses
# an @rpath install name; a Homebrew or hand-built one records an absolute path,
# which dyld would honor instead — ignoring the embedded copy and failing on
# other Macs with "Library not loaded: /abs/path/…". When a framework is embedded
# we rewrite the binary's recorded SDL2 reference (and the framework's own id) to
# @rpath, and make sure the @executable_path/../Frameworks rpath is present.
if [ -d "$app/Contents/Frameworks/SDL2.framework" ] && command -v otool >/dev/null 2>&1; then
	macbin="$app/Contents/MacOS/$name"
	cur="$(otool -L "$macbin" 2>/dev/null | awk '/SDL2\.framework/ {print $1; exit}')" || true
	if [ -n "$cur" ]; then
		# The path from "SDL2.framework" onward (keeps the version letter),
		# prefixed with @rpath — e.g. @rpath/SDL2.framework/Versions/A/SDL2.
		suffix="SDL2.framework${cur#*SDL2.framework}"
		want="@rpath/$suffix"
		if [ "$cur" != "$want" ]; then
			install_name_tool -change "$cur" "$want" "$macbin"
			echo "macapp: rewrote SDL2 reference: $cur -> $want"
		fi
		fwbin="$app/Contents/Frameworks/$suffix"
		if [ -f "$fwbin" ]; then
			install_name_tool -id "$want" "$fwbin" 2>/dev/null || true
		fi
	fi
	# Idempotent: errors ("would duplicate") when the build already added it.
	install_name_tool -add_rpath @executable_path/../Frameworks "$macbin" 2>/dev/null || true
fi

icontag=""
if [ -f "$icns" ]; then
	cp "$icns" "$app/Contents/Resources/$name.icns"
	icontag="<key>CFBundleIconFile</key><string>$name</string>"
else
	echo "macapp: no icon at $icns (add assets/mew.icns or assets/mew.png); using the default icon" >&2
fi

# Ship mew's support tree (grammar pack, help manual) into the bundle's
# Contents/Resources — this is the on-disk "system resource" layer the mew:
# filesystem falls back to on macOS (<mew.app>/Contents/Resources), sitting
# above the copy embedded in the binary. Shipping it on disk keeps the files
# discoverable and updatable without a rebuild. Its subtrees (syntax/, help/,
# …) land directly under Contents/Resources, alongside the icon. MACAPP_RESOURCES
# overrides the source; the default is resolved relative to this script so it
# works regardless of the caller's working directory.
res_src="${MACAPP_RESOURCES:-$(cd "$(dirname "$0")/.." && pwd)/internal/editor/resources}"
if [ -d "$res_src" ]; then
	cp -R "$res_src/." "$app/Contents/Resources/"
	echo "macapp: copied resources from $res_src"
else
	echo "macapp: no resources tree at $res_src — bundle relies on the embedded copy" >&2
fi

# Best-effort version from the BUNDLED binary's --version line ("mew 0.3.1 (…)").
# Run the copy inside the bundle, not the loose $bin: a universal build links
# SDL2 with an @executable_path/../Frameworks rpath that only resolves within the
# bundle (dyld loads SDL2 at startup even for --version), so the loose binary
# would abort. Never fatal — fall back to "0".
ver="$("$app/Contents/MacOS/$name" --version 2>/dev/null | awk '{print $2; exit}')" || true
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
	if [ -n "${CODESIGN_ID:-}" ]; then
		# Distribution signing with a Developer ID Application identity: hardened
		# runtime (required for notarization) + a secure timestamp, signing nested
		# code (the embedded framework) BEFORE the app that contains it. Signing
		# the framework with the same identity keeps library validation happy, so
		# no entitlements are needed for a plain SDL app. Notarize + staple after
		# (make notarize). Failures here are fatal — you want to know.
		fw="$app/Contents/Frameworks/SDL2.framework"
		if [ -d "$fw" ]; then
			codesign --force --options runtime --timestamp --sign "$CODESIGN_ID" "$fw"
		fi
		codesign --force --options runtime --timestamp --sign "$CODESIGN_ID" "$app"
		codesign --verify --deep --strict "$app"
		echo "macapp: signed $app with '$CODESIGN_ID' (hardened runtime); notarize next"
	elif codesign --force --deep --sign - "$app" >/dev/null 2>&1; then
		echo "macapp: ad-hoc signed $app (local use only — set CODESIGN_ID to distribute)"
	else
		echo "macapp: codesign failed; the bundle is unsigned (may not launch on Apple Silicon)" >&2
	fi
fi

echo "macapp: built $app"
