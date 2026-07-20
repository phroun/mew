package editor

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// expandTilde resolves a leading ~ (current user) or ~name (a named user) path
// segment to the matching home directory, cross-platform: "~" and "~/rest" use
// currentHome (falling back to os.UserHomeDir when it is empty), while "~name"
// and "~name/rest" resolve through os/user. It returns the path unchanged with
// ok=false when there is no leading tilde or a named user cannot be resolved.
// The tail is rejoined with filepath.Join so separators normalize per platform.
func expandTilde(path, currentHome string) (string, bool) {
	if path == "" || path[0] != '~' {
		return path, false
	}
	// Split "~seg" from the "/rest" tail at the first separator (accept both /
	// and \ so a Windows-style entry works too).
	seg, rest := path[1:], ""
	if i := strings.IndexAny(path, `/\`); i >= 0 {
		seg, rest = path[1:i], path[i+1:]
	}
	var home string
	if seg == "" {
		home = currentHome
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil || h == "" {
				return path, false
			}
			home = h
		}
	} else {
		u, err := user.Lookup(seg)
		if err != nil || u.HomeDir == "" {
			return path, false
		}
		home = u.HomeDir
	}
	if rest == "" {
		return home, true
	}
	return filepath.Join(home, rest), true
}

// osExpand expands a leading ~ / ~user against the OS's own home resolution, for
// the OS-backed FileSystem below. Non-tilde paths pass through unchanged.
func osExpand(p string) string {
	if e, ok := expandTilde(p, ""); ok {
		return e
	}
	return p
}

// FileSystem is the minimal set of callbacks mew needs for its document file
// scaffolding: loading files into buffers, saving buffers, inserting file
// contents, writing blocks, and (for autocompletion) listing files. Hosts
// embedding mew as a library can substitute a virtual implementation, stub
// individual operations, or disable them by returning errors.
//
// A host may also implement the optional Statter and DirGlobber capabilities
// below; mew uses them when present and degrades gracefully when not (so
// existing implementations keep working unchanged).
type FileSystem interface {
	// ReadFile returns the contents of the named file.
	ReadFile(name string) ([]byte, error)

	// WriteFile writes data to the named file, creating it if necessary.
	WriteFile(name string, data []byte) error

	// Glob returns the names of files matching the pattern (in
	// path/filepath.Match syntax), for filename autocompletion and listings.
	Glob(pattern string) ([]string, error)
}

// FileInfo is the slice of file metadata mew's abstraction exposes.
type FileInfo struct {
	Path    string // the path this describes (as queried or matched)
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// Statter is an optional FileSystem capability: metadata for one path. When a
// FileSystem does not implement it, mew simply does without (e.g. filename
// completion omits directory markers).
type Statter interface {
	Stat(name string) (FileInfo, error)
}

// DirGlobber is an optional FileSystem capability: a Glob that returns each
// match's metadata in a single call, so a caller learns which matches are
// directories without a Stat round-trip per result. A host backed by a
// directory listing that already knows entry types should implement this;
// mew otherwise falls back to Glob followed by Stat (or plain Glob).
type DirGlobber interface {
	GlobStat(pattern string) ([]FileInfo, error)
}

// osFileSystem is the default FileSystem backed by the real operating system.
// It implements Statter and DirGlobber.
type osFileSystem struct{}

func (osFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(osExpand(name))
}

func (osFileSystem) WriteFile(name string, data []byte) error {
	return os.WriteFile(osExpand(name), data, 0644)
}

func (osFileSystem) Glob(pattern string) ([]string, error) {
	return filepath.Glob(osExpand(pattern))
}

func (osFileSystem) Stat(name string) (FileInfo, error) {
	name = osExpand(name)
	fi, err := os.Stat(name)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Path: name, IsDir: fi.IsDir(), Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

func (fs osFileSystem) GlobStat(pattern string) ([]FileInfo, error) {
	matches, err := filepath.Glob(osExpand(pattern))
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(matches))
	for _, m := range matches {
		if info, err := fs.Stat(m); err == nil {
			out = append(out, info)
		} else {
			out = append(out, FileInfo{Path: m}) // best effort: type unknown
		}
	}
	return out, nil
}

// OSFileSystem returns the default FileSystem implementation backed by the
// real operating system.
func OSFileSystem() FileSystem {
	return osFileSystem{}
}
