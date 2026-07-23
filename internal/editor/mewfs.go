package editor

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/config"
)

// The "mew:" scheme addresses mew's own support tree — what used to be spelled
// "~/.mew/": editor.conf, profile.mew, the syntax/ grammars, the help manual,
// native lock files, crash dumps. A host chooses where the WRITABLE tree lives:
//
//   - default (local): "mew:///x" maps to <home>/.mew/x on the real OS, where
//     <home> is the host-overridable home directory (WithHomeDir, else the OS
//     UserHomeDir).
//   - virtualized: with WithMewFileSystem, "mew:///x" is handed to the host's
//     FileSystem (scheme intact), so the host owns the tree.
//
// LAYERED READS. The mew: tree is an overlay: a read (ReadFile/Stat/IsDir/Glob)
// falls through three layers in order —
//
//	1. the USER layer (writable): <home>/.mew (local) or the host FS (virtual)
//	2. the SYSTEM layer (read-only): the [storage] resources= directory, else
//	   the OS convention (/usr/share/mew, %ProgramFiles%\mew\Resources,
//	   <mew.app>/Contents/Resources) — local installs only
//	3. the EMBEDDED layer (read-only): the tree compiled into the binary
//	   (resources/syntax/*.jsf, resources/help/*, …)
//
// so a fresh install has working grammars and help without copying anything
// into ~/.mew, yet the user's own file always shadows the shipped one. WRITES
// only ever touch the user layer, so saving a shipped page creates a private
// ~/.mew copy (a shadow) rather than trying to overwrite a read-only original.
//
// Either way a mew: path is confined to its root: a ".." can never walk above
// <home>/.mew (local) or above "mew:///" (virtual). Confinement happens by
// cleaning the path as if rooted at "/", which drops any leading "..".
//
// Spelling: everything mew PRODUCES — host FileSystem calls, config paths,
// listings, canonical identities — uses "mew:///x", the empty-authority form,
// so the authority slot stays open for addressing other instances later
// (mew://<authority>/x). On INPUT, confine() accepts the user-notation
// spellings the reference spec allows ("mew:/x" is the rooted-no-authority
// form) and normalizes them all to the same confined path.

// mewVFS resolves and accesses the mew: tree.
type mewVFS struct {
	fs        FileSystem // USER layer: host FS (virtual) or the OS-backed FS (local)
	virtual   bool
	localRoot string   // <home>/.mew, when !virtual ("" if home is unknown)
	sysDirs   []string // SYSTEM layer: read-only resource directories (local mode)
}

func newMewVFS(cfg *Config) *mewVFS {
	if cfg.MewFS != nil {
		// A virtualized tree owns its own layout; the embedded layer still backs
		// reads as the last resort (a host with no ~/.mew help still gets it).
		return &mewVFS{fs: cfg.MewFS, virtual: true}
	}
	return &mewVFS{
		fs:        osFileSystem{},
		localRoot: filepath.Join(hostHome(cfg), ".mew"),
		sysDirs:   systemResourceDirs(""),
	}
}

// setSystemResources refines the system (read-only) resource layer once the
// configuration is loaded, honoring a [storage] resources= override. It is a
// no-op for a virtualized tree. Called after config load, so the OS-default
// dirs seeded at construction still back the config read itself.
func (v *mewVFS) setSystemResources(override string) {
	if v.virtual {
		return
	}
	v.sysDirs = systemResourceDirs(override)
}

// hostHome is the home directory mew resolves ~ and the local mew: root
// against: the host override when set, else the OS user home ("" if unknown).
func hostHome(cfg *Config) string {
	if cfg != nil && cfg.HomeDir != "" {
		return cfg.HomeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// isMewPath reports whether a path addresses the mew: tree.
func isMewPath(p string) bool { return strings.HasPrefix(p, "mew:") }

// confine reduces the part after "mew:" to a clean, root-relative path that can
// never escape upward: cleaning as an absolute path collapses "." and drops any
// ".." that would rise above the root.
func confine(mewPath string) string {
	rel := strings.TrimPrefix(mewPath, "mew:")
	rel = strings.TrimPrefix(rel, "/")
	clean := path.Clean("/" + rel) // "/a/../../b" -> "/b"; "/.." -> "/"
	return strings.TrimPrefix(clean, "/")
}

// name returns the concrete name to hand the underlying FileSystem for a mew:
// path: the confined "mew:///rel" (virtual) or <localRoot>/rel (local).
// Returns ("", false) in local mode when the home root is unknown.
func (v *mewVFS) name(mewPath string) (string, bool) {
	rel := confine(mewPath)
	if v.virtual {
		return "mew:///" + rel, true
	}
	if v.localRoot == "" {
		return "", false
	}
	return filepath.Join(v.localRoot, filepath.FromSlash(rel)), true
}

func (v *mewVFS) ReadFile(mewPath string) ([]byte, error) {
	// User layer first (the writable tree).
	if n, ok := v.name(mewPath); ok {
		if data, err := v.fs.ReadFile(n); err == nil {
			return data, nil
		}
	}
	// System + embedded fallback layers.
	if data, ok := v.readFallback(confine(mewPath)); ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

// readFallback reads a confined rel path from the read-only layers below the
// user tree: the system resource directories (local mode), then the embedded
// tree. ok=false when no layer has it.
func (v *mewVFS) readFallback(rel string) ([]byte, bool) {
	for _, dir := range v.sysDirs {
		if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return data, true
		}
	}
	return readEmbeddedResource(rel)
}

func (v *mewVFS) WriteFile(mewPath string, data []byte) error {
	n, ok := v.name(mewPath)
	if !ok {
		return os.ErrInvalid
	}
	// In local mode create the parent directory; a host FS owns its own layout.
	if !v.virtual {
		if err := os.MkdirAll(filepath.Dir(n), 0o755); err != nil {
			return err
		}
	}
	return v.fs.WriteFile(n, data)
}

// LocalPath returns the real OS path backing a mew: path in LOCAL mode
// (<home>/.mew/rel), or ok=false when the tree is virtualized (no real path
// exists) or the home root is unknown.
func (v *mewVFS) LocalPath(mewPath string) (string, bool) {
	if v.virtual || v.localRoot == "" {
		return "", false
	}
	return filepath.Join(v.localRoot, filepath.FromSlash(confine(mewPath))), true
}

// Glob lists mew: entries matching a confined pattern across ALL layers,
// returned as "mew:///rel" paths and de-duplicated by rel (a user-layer entry
// shadows a shipped one of the same name). Used by the wiki resolver to match
// page ids against the files actually present in a mew:-hosted wiki tree, so a
// shipped help page lists exactly as a user's own would.
func (v *mewVFS) Glob(mewPattern string) ([]string, error) {
	relPattern := confine(mewPattern)
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, "mew:///"+rel)
	}

	// User layer.
	if n, ok := v.name(mewPattern); ok {
		if matches, err := v.fs.Glob(n); err == nil {
			for _, m := range matches {
				if v.virtual {
					add(confine(m)) // host results are already mew: names
					continue
				}
				if rel, err := filepath.Rel(v.localRoot, m); err == nil {
					add(filepath.ToSlash(rel))
				}
			}
		}
	}
	// System layer(s).
	for _, dir := range v.sysDirs {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(relPattern)))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if rel, err := filepath.Rel(dir, m); err == nil {
				add(filepath.ToSlash(rel))
			}
		}
	}
	// Embedded layer.
	for _, rel := range globEmbeddedResource(relPattern) {
		add(rel)
	}
	return out, nil
}

// globEmbeddedResource matches a confined "dir/pattern" against the embedded
// tree's directory listing (only the last segment is a glob pattern, which is
// all the wiki resolver's "dir/*" listings need).
func globEmbeddedResource(relPattern string) []string {
	dir, pat := path.Split(relPattern)
	dir = strings.TrimSuffix(dir, "/")
	var out []string
	for _, name := range listEmbeddedResource(dir) {
		if ok, _ := path.Match(pat, name); ok {
			out = append(out, path.Join(dir, name))
		}
	}
	return out
}

// Stat reports metadata for a mew: path across all layers (exists=false when
// no layer has it). Plain FileSystems without a Statter degrade to a read
// probe, which cannot see directories.
func (v *mewVFS) Stat(mewPath string) (info FileInfo, exists bool) {
	if n, ok := v.name(mewPath); ok {
		if st, ok := v.fs.(Statter); ok {
			if fi, err := st.Stat(n); err == nil {
				return fi, true
			}
		} else if _, err := v.fs.ReadFile(n); err == nil {
			return FileInfo{Path: n}, true
		}
	}
	if isDir, ok := v.statFallback(confine(mewPath)); ok {
		return FileInfo{Path: mewPath, IsDir: isDir}, true
	}
	return FileInfo{}, false
}

// IsDir reports whether a mew: path is an existing directory in any layer
// (best effort: false when no layer can answer).
func (v *mewVFS) IsDir(mewPath string) bool {
	if n, ok := v.name(mewPath); ok {
		if st, ok := v.fs.(Statter); ok {
			if info, err := st.Stat(n); err == nil {
				return info.IsDir
			}
		}
	}
	isDir, ok := v.statFallback(confine(mewPath))
	return ok && isDir
}

// statFallback reports existence and directory-ness of a confined rel path in
// the read-only layers below the user tree.
func (v *mewVFS) statFallback(rel string) (isDir, exists bool) {
	for _, dir := range v.sysDirs {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return info.IsDir(), true
		}
	}
	return statEmbeddedResource(rel)
}

// listFallback returns the base names directly under a confined rel directory
// across the read-only layers (system dirs then embedded), de-duplicated.
func (v *mewVFS) listFallback(rel string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, dir := range v.sysDirs {
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		for _, ent := range entries {
			add(ent.Name())
		}
	}
	for _, name := range listEmbeddedResource(rel) {
		add(name)
	}
	return out
}

// relForLocal maps a real OS path back to its confined mew: rel path when it
// lies under the local user root (<home>/.mew), so the editor's document-FS
// operations — which see a mew: page as a real ~/.mew/... path after the
// canonical-URL translation — can consult the read-only fallback layers for a
// page the user has no local copy of. ok=false in virtual mode, when the root
// is unknown, or when realPath is outside the tree.
func (v *mewVFS) relForLocal(realPath string) (string, bool) {
	if v.virtual || v.localRoot == "" {
		return "", false
	}
	rel, err := filepath.Rel(v.localRoot, realPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	if rel == "." {
		rel = ""
	}
	return rel, true
}

// fallbackForLocal reads a real ~/.mew/... path from the read-only fallback
// layers (system + embedded), used when the file itself is absent from the
// user tree. ok=false when the path is outside the tree or no layer has it.
func (v *mewVFS) fallbackForLocal(realPath string) ([]byte, bool) {
	rel, ok := v.relForLocal(realPath)
	if !ok {
		return nil, false
	}
	return v.readFallback(rel)
}

// statFallbackForLocal reports existence/dir-ness of a real ~/.mew/... path in
// the read-only fallback layers.
func (v *mewVFS) statFallbackForLocal(realPath string) (isDir, exists bool) {
	rel, ok := v.relForLocal(realPath)
	if !ok {
		return false, false
	}
	return v.statFallback(rel)
}

// listFallbackForLocal returns the base names under a real ~/.mew/... directory
// present only in the read-only fallback layers.
func (v *mewVFS) listFallbackForLocal(realDir string) []string {
	rel, ok := v.relForLocal(realDir)
	if !ok {
		return nil
	}
	return v.listFallback(rel)
}

// makeConfigFileIO builds the config.Manager's file access: mew:/ paths (user
// editor.conf, profile, angle-bracket includes) go through the mew tree;
// everything else (project .mew files, relative includes) through the document
// FS. Directory tests use the document FS's Statter when it has one.
func makeConfigFileIO(docFS FileSystem, mew *mewVFS) config.FileIO {
	return config.FileIO{
		Read: func(p string) ([]byte, error) {
			if isMewPath(p) {
				return mew.ReadFile(p)
			}
			return docFS.ReadFile(p)
		},
		Write: func(p string, data []byte) error {
			if isMewPath(p) {
				return mew.WriteFile(p, data)
			}
			return docFS.WriteFile(p, data)
		},
		IsDir: func(p string) bool {
			if isMewPath(p) {
				return mew.IsDir(p)
			}
			if st, ok := docFS.(Statter); ok {
				if info, err := st.Stat(p); err == nil {
					return info.IsDir
				}
			}
			return false
		},
	}
}
