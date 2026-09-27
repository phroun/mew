package text

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gtfont "github.com/go-text/typesetting/font"
)

// Dynamic font registration and name-based lookup. Fonts enter the engine
// either by explicit file path (a [fonts] entry, Decision B(i)) or by family
// NAME, found by scanning the OS font directories plus any extra search paths
// (a [window] fonts_path, Decision B(ii)). The embedded Noto/Go faces remain
// the deterministic fallback core; loaded fonts only extend it.

// AddFontSearchPath adds a directory to those RegisterFontByName scans (beyond
// the OS defaults). Invalidates the lazily-built name index.
func (e *Engine) AddFontSearchPath(dir string) {
	if dir == "" {
		return
	}
	e.db.mu.Lock()
	e.db.searchPaths = append(e.db.searchPaths, dir)
	e.db.nameIndex = nil
	e.db.mu.Unlock()
}

// RegisterFontFile registers every face in a TTF/OTF/TTC file under family, at
// each face's own bold/italic aspect, and bumps the engine epoch. The last
// path component is used for the family name when family is empty.
func (e *Engine) RegisterFontFile(family, path string) error {
	faces, err := loadFaces(path)
	if err != nil {
		return err
	}
	if family == "" {
		family = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	for _, f := range faces {
		e.db.registerFace(family, faceAspect(f), f)
	}
	e.bumpEpoch()
	return nil
}

// RegisterFontByName scans the search paths and OS font directories for faces
// whose family name matches want (space- and case-insensitive), registering
// each under alias `as` (typically want itself). Returns true if any matched.
func (e *Engine) RegisterFontByName(as, want string) bool {
	norm := gtfont.NormalizeFamily(want)
	got := false
	for _, p := range e.db.indexLookup(norm) {
		faces, err := loadFaces(p)
		if err != nil {
			continue
		}
		for _, f := range faces {
			if gtfont.NormalizeFamily(f.Describe().Family) != norm {
				continue // a TTC may hold unrelated families
			}
			e.db.registerFace(as, faceAspect(f), f)
			got = true
		}
	}
	if got {
		e.bumpEpoch()
	}
	return got
}

// UseFont re-points an alias (e.g. "ui-term") at an ordered list of font
// names, LOADING any name not yet registered by scanning for it. resolve then
// uses the first that is available, so the list doubles as a fallback chain.
// Returns whether the first (preferred) name resolved to a real family.
func (e *Engine) UseFont(alias string, names ...string) bool {
	for _, n := range names {
		if !e.db.hasFamilyOrAlias(n) {
			e.RegisterFontByName(n, n)
		}
	}
	e.SetFontAlias(alias, names...)
	if len(names) == 0 {
		return false
	}
	return e.db.hasFamilyOrAlias(names[0])
}

func (e *Engine) bumpEpoch() {
	e.mu.Lock()
	e.cache.clear()
	e.epoch++
	e.mu.Unlock()
}

func (db *fontDB) hasFamilyOrAlias(name string) bool {
	key := canonical(name)
	db.mu.RLock()
	defer db.mu.RUnlock()
	if _, ok := db.families[key]; ok {
		return true
	}
	_, ok := db.aliases[key]
	return ok
}

// loadFaces parses a font file into its faces (a TTC/OTC may hold several).
func loadFaces(path string) ([]*gtfont.Face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ttc", ".otc":
		return gtfont.ParseTTC(bytes.NewReader(data))
	default:
		f, err := gtfont.ParseTTF(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return []*gtfont.Face{f}, nil
	}
}

// faceAspect derives our bold/italic aspect from a parsed face's metadata.
func faceAspect(f *gtfont.Face) Aspect {
	d := f.Describe()
	return Aspect{
		Bold:   d.Aspect.Weight >= gtfont.WeightBold,
		Italic: d.Aspect.Style != gtfont.StyleNormal,
	}
}

// indexLookup returns the file paths for a normalized family name, building the
// name index once (lazily) from the search paths and OS font directories.
func (db *fontDB) indexLookup(norm string) []string {
	db.mu.Lock()
	if db.nameIndex == nil {
		dirs := append(append([]string{}, db.searchPaths...), osFontDirs()...)
		db.nameIndex = buildNameIndex(dirs)
	}
	paths := db.nameIndex[norm]
	db.mu.Unlock()
	return paths
}

// buildNameIndex walks dirs for font files and maps each face's normalized
// family name to the files that provide it. A file it cannot name is skipped.
//
// The names are read from each file's name table rather than by parsing the
// font (see fontnames.go). Parsing every font on the machine to learn what
// each is called cost over a second here for 49 files, nearly all of it
// reading bytes that say nothing about the name; reading the one table is
// three orders of magnitude cheaper and answers the same.
//
// What was read is remembered between runs (see fontcache.go), so a file whose
// size and time have not changed is not opened at all. Only what the walk turns
// up is kept, and the cache is written again only when it says something new.
//
// There is no cap on how many files are looked at. There was one, at six
// thousand, because parsing that many would have been unbearable -- and it
// meant a family past the cap was silently unfindable, which is a worse
// failure than a slow start.
func buildNameIndex(dirs []string) map[string][]string {
	index := map[string][]string{}
	known := readFontCache(fontCachePath())
	found := make(map[string]fontNames, len(known))
	seenFile := map[string]bool{}
	asRemembered := true

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			if seenFile[path] {
				return nil
			}
			seenFile[path] = true
			info, err := d.Info()
			if err != nil {
				return nil
			}
			names := fontNames{size: info.Size(), modified: info.ModTime().UnixNano()}
			was, remembered := known[path]
			if remembered && was.size == names.size && was.modified == names.modified {
				names.families = was.families
			} else {
				families, err := familiesIn(path)
				if err != nil {
					// A file that cannot be named is not written down: it may
					// be one this cannot read today and can tomorrow, and a
					// chmod does not change the time the cache compares.
					return nil
				}
				names.families = families
				asRemembered = false
			}
			found[path] = names
			for _, family := range names.families {
				norm := gtfont.NormalizeFamily(family)
				if norm == "" {
					continue
				}
				index[norm] = appendUnique(index[norm], path)
			}
			return nil
		})
	}

	if !asRemembered || len(found) != len(known) {
		writeFontCache(fontCachePath(), found)
	}
	return index
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// osFontDirs returns the platform's standard font directories.
func osFontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		dirs := []string{"/System/Library/Fonts", "/Library/Fonts"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		return dirs
	case "windows":
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		dirs := []string{filepath.Join(windir, "Fonts")}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}
		return dirs
	default: // linux and other unixes
		dirs := []string{"/usr/share/fonts", "/usr/local/share/fonts"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts"))
		}
		return dirs
	}
}
