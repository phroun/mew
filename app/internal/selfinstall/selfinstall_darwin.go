//go:build darwin

// macOS implementation of the self-installer: copy the running .app bundle into
// an Applications folder. Like the Windows build, "already installed" is judged
// from the binary's location — here, whether it runs from inside an Applications
// folder — not a stored flag. It is per-user with no privilege prompt: it
// installs to /Applications when that happens to be writable, otherwise to
// ~/Applications, which macOS treats as a valid app location.
package selfinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const installAppName = "mew"

// Available reports that the installer is usable on this platform (macOS).
func Available() bool { return true }

// InstallLocationPhrase is a human phrase for where Install puts mew, for the
// welcome copy ("Install adds mew to <phrase>…").
func InstallLocationPhrase() string { return "your Applications folder" }

// FirstRunDone reports whether this copy looks already-installed, judged purely
// from where its binary lives: true when some ancestor folder is named
// "Applications" (the .app bundle sits in /Applications or ~/Applications). A
// copy run from a download folder (~/Downloads/…) has no such ancestor, so it
// still offers to install. If the location can't be determined, it errs toward
// offering the welcome.
func FirstRunDone() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for dir := filepath.Dir(exe); ; {
		if strings.EqualFold(filepath.Base(dir), "Applications") {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// Install copies the running .app bundle into an Applications folder and returns
// the installed inner binary to relaunch (the caller launches it and quits). It
// installs to /Applications when that is writable without a prompt, else to the
// per-user ~/Applications.
func Install() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	bundle := enclosingAppBundle(exe)
	if bundle == "" {
		return "", fmt.Errorf("not running from a .app bundle, nothing to install (build one with `make macapp`)")
	}

	destDir := installBundleDir()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, filepath.Base(bundle))

	// Replace any existing install so a stale bundle doesn't linger.
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("remove old %s: %w", dest, err)
	}
	if err := copyTree(bundle, dest); err != nil {
		return "", fmt.Errorf("copy bundle: %w", err)
	}
	fmt.Printf("installed %s\n", dest)

	// The inner binary keeps its name inside the copied bundle.
	return filepath.Join(dest, "Contents", "MacOS", filepath.Base(exe)), nil
}

// Uninstall removes the installed bundle from /Applications and ~/Applications
// (best-effort).
func Uninstall() error {
	name := installAppName + ".app"
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if err := os.RemoveAll(p); err != nil {
			fmt.Fprintf(os.Stderr, "mew: could not remove %s: %v\n", p, err)
		} else {
			fmt.Printf("removed %s\n", p)
		}
	}
	return nil
}

// enclosingAppBundle returns the nearest ancestor directory ending in ".app", or
// "" if the binary is not inside a bundle.
func enclosingAppBundle(p string) string {
	for dir := filepath.Dir(p); ; {
		if strings.HasSuffix(strings.ToLower(dir), ".app") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// installBundleDir is /Applications when writable without a prompt, else the
// per-user ~/Applications.
func installBundleDir() string {
	if dirWritable("/Applications") {
		return "/Applications"
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Applications")
	}
	return "/Applications"
}

func dirWritable(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".mew-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// copyTree recursively copies the .app bundle at src to dst, preserving file
// modes and symlinks (bundles can carry relative symlinks, e.g. frameworks).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	clErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return clErr
}
