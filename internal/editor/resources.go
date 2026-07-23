package editor

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// embeddedResources is mew's statically-linked support tree: the MIT-licensed
// grammar pack (resources/syntax/*.jsf), the built-in help manual
// (resources/help/*), and anything else that should ship inside the binary as
// the LAST-RESORT layer of the mew: filesystem. It is read-only and always
// present, so a fresh install has working syntax highlighting and help without
// writing a single file into the user's ~/.mew.
//
//go:embed resources
var embeddedResources embed.FS

// embeddedResourcePrefix is the directory embeddedResources is rooted at: a
// mew: rel path "syntax/go.jsf" lives at "resources/syntax/go.jsf" in the
// embed.
const embeddedResourcePrefix = "resources"

// readEmbeddedResource reads a mew: rel path ("syntax/go.jsf") from the
// embedded tree, or ok=false when it is absent.
func readEmbeddedResource(rel string) ([]byte, bool) {
	data, err := embeddedResources.ReadFile(embeddedResourcePrefix + "/" + rel)
	if err != nil {
		return nil, false
	}
	return data, true
}

// statEmbeddedResource reports whether a mew: rel path exists in the embedded
// tree, and whether it is a directory.
func statEmbeddedResource(rel string) (isDir, exists bool) {
	name := embeddedResourcePrefix
	if rel != "" {
		name += "/" + rel
	}
	info, err := fs.Stat(embeddedResources, name)
	if err != nil {
		return false, false
	}
	return info.IsDir(), true
}

// listEmbeddedResource returns the base names of the entries directly under a
// mew: rel directory in the embedded tree (nil when it is not a directory).
func listEmbeddedResource(rel string) []string {
	name := embeddedResourcePrefix
	if rel != "" {
		name += "/" + rel
	}
	entries, err := fs.ReadDir(embeddedResources, name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, ent := range entries {
		out = append(out, ent.Name())
	}
	return out
}

// systemResourceDirs is the ordered list of read-only system resource
// directories the mew: filesystem falls back to after the user's ~/.mew and
// before the embedded tree. An explicit [storage] resources= (override) wins
// outright; otherwise the OS conventions are probed and only existing
// directories are kept.
//
// The conventions:
//
//	linux/unix   /usr/local/share/mew, /usr/share/mew
//	windows      %ProgramFiles%\mew\Resources
//	darwin       <mew.app>/Contents/Resources (derived from the executable)
//
// Only local (real-OS) installs have system directories; a virtualized mew
// tree owns its own layout, so this returns nil there.
func systemResourceDirs(override string) []string {
	if override = strings.TrimSpace(override); override != "" {
		if dirExists(override) {
			return []string{override}
		}
		return nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepath.Join(pf, "mew", "Resources"))
		}
	case "darwin":
		// A .app bundle keeps resources beside the executable:
		// <bundle>.app/Contents/MacOS/mew -> <bundle>.app/Contents/Resources.
		if exe, err := os.Executable(); err == nil {
			macos := filepath.Dir(exe)
			if strings.EqualFold(filepath.Base(macos), "MacOS") {
				candidates = append(candidates, filepath.Join(filepath.Dir(macos), "Resources"))
			}
		}
		candidates = append(candidates, "/usr/local/share/mew", "/usr/share/mew")
	default:
		candidates = append(candidates, "/usr/local/share/mew", "/usr/share/mew")
	}
	var out []string
	for _, d := range candidates {
		if dirExists(d) {
			out = append(out, d)
		}
	}
	return out
}

// dirExists reports whether path names an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
