#!/usr/bin/env bash
#
# fetch-wgpu.sh — download the wgpu-native runtime and drop its shared library
# where the Windows build embeds it (app/internal/wgpuembed), so mew-sdl.exe
# ships its GPU runtime inside the binary. NOT committed (gitignored); wgpu-native
# is MIT/Apache-2.0, so redistribution is fine. Mirrors scripts/fetch-sdl3.sh.
#
#   scripts/fetch-wgpu.sh <wgpu-version> <goos> <goarch> <dest-file>
#
# The version tracks what go-webgpu targets (setup.Version); override with
# WGPU_URL if the release asset naming ever changes. A present dest is reused.
set -euo pipefail

ver="${1:?usage: fetch-wgpu.sh <version> <goos> <goarch> <dest-file>}"
goos="${2:?}"
goarch="${3:?}"
dest="${4:?}"

if [ -f "$dest" ]; then
	echo "fetch-wgpu: using cached $dest"
	exit 0
fi

case "$goarch" in
amd64) arch=x86_64 ;;
arm64) arch=aarch64 ;;
*) echo "fetch-wgpu: unsupported arch $goarch" >&2; exit 1 ;;
esac
case "$goos" in
windows) zip="wgpu-windows-$arch-msvc-release.zip"; libInZip="lib/wgpu_native.dll" ;;
*) echo "fetch-wgpu: only windows is wired up (got $goos)" >&2; exit 1 ;;
esac

url="${WGPU_URL:-https://github.com/gfx-rs/wgpu-native/releases/download/$ver/$zip}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "fetch-wgpu: downloading $url"
if ! curl -fL --retry 3 -o "$tmp/w.zip" "$url"; then
	echo "fetch-wgpu: download failed." >&2
	echo "  tried: $url" >&2
	echo "  Pin WGPU_VERSION or set WGPU_URL to the right asset on" >&2
	echo "  https://github.com/gfx-rs/wgpu-native/releases." >&2
	exit 1
fi
mkdir -p "$(dirname "$dest")"
unzip -o -j "$tmp/w.zip" "$libInZip" -d "$tmp" >/dev/null
cp "$tmp/$(basename "$libInZip")" "$dest"
echo "fetch-wgpu: installed $dest ($(wc -c < "$dest") bytes)"
