package editor

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/config"
)

// The "mew:" scheme addresses mew's own support tree — what used to be spelled
// "~/.mew/": editor.conf, profile.mew, the syntax/ grammars, native lock files,
// crash dumps. A host chooses where that tree lives:
//
//   - default (local): "mew:///x" maps to <home>/.mew/x on the real OS, where
//     <home> is the host-overridable home directory (WithHomeDir, else the OS
//     UserHomeDir).
//   - virtualized: with WithMewFileSystem, "mew:///x" is handed to the host's
//     FileSystem (scheme intact), so the host owns the tree.
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
	fs        FileSystem // host FS (virtual) or the OS-backed FS (local)
	virtual   bool
	localRoot string // <home>/.mew, when !virtual ("" if home is unknown)
}

func newMewVFS(cfg *Config) *mewVFS {
	if cfg.MewFS != nil {
		return &mewVFS{fs: cfg.MewFS, virtual: true}
	}
	return &mewVFS{fs: osFileSystem{}, localRoot: filepath.Join(hostHome(cfg), ".mew")}
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
	n, ok := v.name(mewPath)
	if !ok {
		return nil, os.ErrNotExist
	}
	return v.fs.ReadFile(n)
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

// Glob lists mew: entries matching a confined pattern, returned as
// "mew:///rel" paths (local results are mapped back out of the on-disk root).
// Used by the wiki resolver to match page ids against the files actually
// present in a mew:-hosted wiki tree.
func (v *mewVFS) Glob(mewPattern string) ([]string, error) {
	n, ok := v.name(mewPattern)
	if !ok {
		return nil, os.ErrNotExist
	}
	matches, err := v.fs.Glob(n)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if v.virtual {
			out = append(out, m) // host results are already mew: names
			continue
		}
		rel, err := filepath.Rel(v.localRoot, m)
		if err != nil {
			continue
		}
		out = append(out, "mew:///"+filepath.ToSlash(rel))
	}
	return out, nil
}

// Stat reports metadata for a mew: path (exists=false when the underlying
// FileSystem cannot answer or the path is absent). Plain FileSystems without
// a Statter degrade to a read probe, which cannot see directories.
func (v *mewVFS) Stat(mewPath string) (info FileInfo, exists bool) {
	n, ok := v.name(mewPath)
	if !ok {
		return FileInfo{}, false
	}
	if st, ok := v.fs.(Statter); ok {
		fi, err := st.Stat(n)
		if err != nil {
			return FileInfo{}, false
		}
		return fi, true
	}
	if _, err := v.fs.ReadFile(n); err == nil {
		return FileInfo{Path: n}, true
	}
	return FileInfo{}, false
}

// IsDir reports whether a mew: path is an existing directory (best effort:
// false when the FileSystem cannot answer).
func (v *mewVFS) IsDir(mewPath string) bool {
	n, ok := v.name(mewPath)
	if !ok {
		return false
	}
	if st, ok := v.fs.(Statter); ok {
		if info, err := st.Stat(n); err == nil {
			return info.IsDir
		}
	}
	return false
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
