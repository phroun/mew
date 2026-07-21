package editor

import (
	"os"
	"path/filepath"
)

// ensureUsefulStartDir runs once at session start. GUI-launched processes
// typically inherit a filesystem root as their working directory ("/" from
// the macOS Finder, a drive root on Windows), which makes every relative
// operation — file prompts, completion, project discovery — useless. When
// the working directory is a root (or unknown), chdir to the first usable of:
//
//  1. the directory of the last main buffer's file, when it exists on disk
//     (a session launched on a document starts beside it);
//  2. [general] startPath (tilde-expanded);
//  3. the user's home directory.
//
// A deliberate working directory — a terminal launch from a project — is
// never overridden, and host-virtualized document filesystems are left
// alone entirely.
func (e *Editor) ensureUsefulStartDir() {
	if !e.osBackedFS() {
		return
	}
	if wd, err := os.Getwd(); err == nil && !isRootDir(wd) {
		return
	}
	chdir := func(dir string) bool {
		if dir == "" {
			return false
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return false
		}
		return os.Chdir(dir) == nil
	}
	if w := e.WindowManager.LastMainWindow(); w != nil && w.Buffer != nil {
		if fn := e.normalizeDocPath(w.Buffer.GetFilename()); fn != "" && !isMewPath(fn) {
			if _, err := os.Stat(fn); err == nil && chdir(filepath.Dir(fn)) {
				return
			}
		}
	}
	if sp := e.LoadedConfig.General.StartPath; sp != "" {
		if ex, ok := expandTilde(sp, e.home); ok {
			sp = ex
		}
		if chdir(sp) {
			return
		}
	}
	chdir(e.home)
}

// isRootDir reports whether a path is a filesystem root ("/" on Unix, a
// drive root like `C:\` on Windows).
func isRootDir(dir string) bool {
	c := filepath.Clean(dir)
	return c == filepath.VolumeName(c)+string(filepath.Separator)
}
