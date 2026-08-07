#!/usr/bin/env bash
#
# fetch-sdl3.sh — download an official universal SDL3 runtime into a local cache
# so `make macapp-universal` can bundle it into mew.app for a self-contained
# installer. NOT committed to the repo (the cache is gitignored); SDL is
# zlib-licensed, so downloading, caching, and redistributing the binary is fine.
#
#   scripts/fetch-sdl3.sh <version> <dest-dir>
#
# macOS: fetches SDL3-<version>.dmg from libsdl.org's GitHub release, mounts it,
# and copies the universal (arm64 + x86_64) SDL3.framework binary out as
# <dest-dir>/libSDL3.dylib. Already-present output is reused (delete it, or the
# dest dir, to force a refresh). Override the URL with SDL3_URL if the release
# asset naming ever changes.
set -euo pipefail

ver="${1:?usage: fetch-sdl3.sh <version> <dest-dir>}"
dest="${2:?usage: fetch-sdl3.sh <version> <dest-dir>}"
out="$dest/libSDL3.dylib"

if [ -f "$out" ]; then
	echo "fetch-sdl3: using cached $out"
	[ -x "$(command -v lipo)" ] && echo "fetch-sdl3: arches: $(lipo -archs "$out" 2>/dev/null)"
	exit 0
fi

os="$(uname -s)"
mkdir -p "$dest"

case "$os" in
Darwin)
	url="${SDL3_URL:-https://github.com/libsdl-org/SDL/releases/download/release-$ver/SDL3-$ver.dmg}"
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	dmg="$tmp/SDL3.dmg"
	echo "fetch-sdl3: downloading $url"
	if ! curl -fL --retry 3 -o "$dmg" "$url"; then
		echo "fetch-sdl3: download failed." >&2
		echo "  tried: $url" >&2
		echo "  Fix by pinning a real release (SDL3_VERSION=<x.y.z>) or setting SDL3_URL" >&2
		echo "  to the exact .dmg asset on https://github.com/libsdl-org/SDL/releases." >&2
		exit 1
	fi
	mnt="$tmp/mnt"
	mkdir -p "$mnt"
	hdiutil attach "$dmg" -nobrowse -quiet -mountpoint "$mnt"
	# The framework's Mach-O binary IS the universal dylib; copy it out under the
	# plain name the host's loader looks for (Contents/Frameworks/libSDL3.dylib).
	# A dmg can carry more than one SDL3 (the framework itself plus a bundled
	# test/demo app with its own thin copy), so pick the FATTEST — the real
	# framework binary is universal; the strays are single-arch. The real binary
	# is a regular file at Versions/<v>/SDL3, so -type f skips the symlinks.
	fw=""; fwn=0
	while IFS= read -r cand; do
		[ -n "$cand" ] || continue
		n=$(lipo -archs "$cand" 2>/dev/null | wc -w | tr -d ' ')
		echo "fetch-sdl3:   candidate $cand [$(lipo -archs "$cand" 2>/dev/null)]"
		if [ "${n:-0}" -gt "$fwn" ]; then fw="$cand"; fwn="$n"; fi
	done < <(find "$mnt" -type f -path '*SDL3.framework/*' -name SDL3 2>/dev/null)
	if [ -z "$fw" ]; then
		echo "fetch-sdl3: SDL3.framework binary not found inside the dmg — its layout is:" >&2
		find "$mnt" -maxdepth 3 2>/dev/null | sed 's/^/  /' >&2
		hdiutil detach "$mnt" -quiet 2>/dev/null || true
		exit 1
	fi
	cp "$fw" "$out"
	# The framework binary records an @rpath/framework install name; rewrite it
	# to a plain name so a dlopen by our explicit bundle path is unambiguous.
	if command -v install_name_tool >/dev/null 2>&1; then
		install_name_tool -id "libSDL3.dylib" "$out" 2>/dev/null || true
	fi
	hdiutil detach "$mnt" -quiet 2>/dev/null || true
	arches="$(lipo -archs "$out" 2>/dev/null || echo '?')"
	echo "fetch-sdl3: extracted SDL3 $ver -> $out (arches: $arches)"
	if ! printf ' %s ' "$arches" | grep -q ' arm64 ' || ! printf ' %s ' "$arches" | grep -q ' x86_64 '; then
		echo "fetch-sdl3: WARNING: the extracted SDL3 is $arches-only, not universal." >&2
		echo "  The bundled .app will run on $arches Macs only. If the candidate list above" >&2
		echo "  showed no fat SDL3, that release's dmg framework is not universal — pin a" >&2
		echo "  different SDL3_VERSION, or supply a universal dylib via MACAPP_SDL3." >&2
	fi
	;;
*)
	echo "fetch-sdl3: only macOS is wired up here (got $os)." >&2
	echo "  Elsewhere the host loads a system SDL3, or point MACAPP_SDL3 at a dylib." >&2
	exit 1
	;;
esac
