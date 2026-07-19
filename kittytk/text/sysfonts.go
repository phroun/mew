package text

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gtfont "github.com/go-text/typesetting/font"

	"github.com/phroun/kittytk/core"
)

// System font fallbacks. The embedded Noto faces remain the
// deterministic core of the fallback chain (D5: layout consults only
// registered fonts); LoadSystemFallbacks appends well-known OS faces
// AFTER them in registration order, so a system face only ever
// resolves runes that nothing embedded covers.

// systemFallbackFiles returns candidate font paths for this OS in
// priority order: broad-coverage text faces, then symbol faces, then
// CJK collections. Missing files are simply skipped.
func systemFallbackFiles() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Helvetica.ttc",
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
			"/System/Library/Fonts/Supplemental/Arial.ttf",
			"/System/Library/Fonts/Apple Symbols.ttf",
			"/System/Library/Fonts/Supplemental/Symbol.ttf",
			"/System/Library/Fonts/Supplemental/Zapf Dingbats.ttf",
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/Supplemental/Songti.ttc",
			"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
		}
	case "windows":
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		names := []string{
			"segoeui.ttf", // Segoe UI
			"arial.ttf",
			"arialuni.ttf", // Arial Unicode MS (when installed)
			"seguisym.ttf", // Segoe UI Symbol
			"seguihis.ttf", // Segoe UI Historic
			"tahoma.ttf",
			"times.ttf",
			"msyh.ttc", // Microsoft YaHei
			"msgothic.ttc",
			"simsun.ttc",
			"malgun.ttf",
			"mingliu.ttc",
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			out = append(out, filepath.Join(windir, "Fonts", n))
		}
		return out
	default: // linux and other unixes
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
			"/usr/share/fonts/TTF/DejaVuSans.ttf", // arch layout
			"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
			"/usr/share/fonts/truetype/liberation2/LiberationSans-Regular.ttf",
			"/usr/share/fonts/truetype/freefont/FreeSans.ttf",
			"/usr/share/fonts/truetype/unifont/unifont.ttf",
			"/usr/share/fonts/truetype/ancient-scripts/Symbola_hint.ttf",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
		}
	}
}

// LoadSystemFallbacks registers whichever of this OS's well-known
// faces exist, at the tail of the per-rune fallback chain. Color
// emoji faces are not attempted (the renderer is monochrome). Parse
// or read failures skip the file. Returns the number of faces
// registered; safe to call more than once.
func (e *Engine) LoadSystemFallbacks() int {
	return e.loadFallbackFiles(systemFallbackFiles())
}

// macMenuFontFiles returns candidate paths for macOS's UI font in priority
// order (San Francisco first, then the classic Helvetica). Empty off macOS.
func macMenuFontFiles() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return []string{
		"/System/Library/Fonts/SFNS.ttf",     // San Francisco (system UI font)
		"/System/Library/Fonts/SFNSText.ttf", // older SF naming
		"/System/Library/Fonts/Helvetica.ttc",
	}
}

// LoadMacMenuFont registers macOS's UI font under core.MacShortcutFontFamily so
// menu shortcuts can address it by name for native rendering. It uses the first
// candidate that parses and returns true; a no-op returning false when none is
// found (off macOS, or the files are absent). Safe to call more than once.
func (e *Engine) LoadMacMenuFont() bool {
	for _, path := range macMenuFontFiles() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var face *gtfont.Face
		if strings.EqualFold(filepath.Ext(path), ".ttc") {
			faces, err := gtfont.ParseTTC(bytes.NewReader(data))
			if err != nil || len(faces) == 0 {
				continue
			}
			face = faces[0]
		} else {
			f, err := gtfont.ParseTTF(bytes.NewReader(data))
			if err != nil {
				continue
			}
			face = f
		}
		e.db.registerFace(core.MacShortcutFontFamily, Aspect{}, face)
		e.mu.Lock()
		e.cache.clear()
		e.epoch++
		e.mu.Unlock()
		return true
	}
	return false
}

func (e *Engine) loadFallbackFiles(paths []string) int {
	loaded := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var face *gtfont.Face
		if strings.EqualFold(filepath.Ext(path), ".ttc") {
			faces, err := gtfont.ParseTTC(bytes.NewReader(data))
			if err != nil || len(faces) == 0 {
				continue
			}
			face = faces[0]
		} else {
			f, err := gtfont.ParseTTF(bytes.NewReader(data))
			if err != nil {
				continue
			}
			face = f
		}
		// Family name "sys:<file>" keeps system faces out of the way
		// of by-name lookups while extending the fallback order.
		e.db.registerFace("sys:"+filepath.Base(path), Aspect{}, face)
		loaded++
	}
	if loaded > 0 {
		e.mu.Lock()
		e.cache.clear()
		e.epoch++
		e.mu.Unlock()
	}
	return loaded
}
