package text

// What the machine's fonts are called, remembered between runs.
//
// Finding a font by name means knowing the name of every font on the machine,
// and a name is only in the file that carries it. Reading the one table that
// holds it is cheap (see fontnames.go), but it is still a seek and a read per
// file, on a machine that may have thousands of them and a disk that may be
// neither local nor fast.
//
// So what was read is written down: for each file, how big it was and when it
// last changed, and what it said it was called. A file that still has that size
// and that time still has those names, and is not opened again. Anything else
// -- a new file, a changed one, one this has never seen -- is read and written
// down in its turn.
//
// Losing this file costs one slower start. It is kept under cache/ for exactly
// that reason: it can be deleted at any moment and nothing is lost but time.
//
// The cache holds what the search paths hold. A file that is no longer found
// under any of them stops being remembered, so the file stays the size of the
// machine's fonts rather than growing with every font that ever passed through.

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// FontCacheEnv overrides the path of the font name cache.
const FontCacheEnv = "KITTYTK_FONT_CACHE"

const fontCacheHeader = "# kittytk font names: <modified> <size> <path> then a tab before each family"

// fontCachePath is where the names are remembered.
func fontCachePath() string {
	if p := os.Getenv(FontCacheEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "cache", "fonts", "index")
}

// configDir mirrors the host's and the clients' rule so everything this
// machine keeps for the toolkit sits in one place.
func configDir() string {
	var base string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		base = x
	} else if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			base = a
		}
	}
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kittytk")
}

// fontNames is what one file was called, and what it looked like when it said
// so.
type fontNames struct {
	size     int64
	modified int64
	families []string
}

// readFontCache reads what previous runs wrote down. A cache that is missing,
// unreadable or garbled is no cache, which costs a slower start and nothing
// else -- so nothing here reports an error.
func readFontCache(path string) map[string]fontNames {
	f, err := os.Open(path)
	if err != nil {
		return map[string]fontNames{}
	}
	defer f.Close()

	out := map[string]fontNames{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		head := strings.SplitN(fields[0], " ", 3)
		if len(head) != 3 {
			continue
		}
		modified, err := strconv.ParseInt(head[0], 10, 64)
		if err != nil {
			continue
		}
		size, err := strconv.ParseInt(head[1], 10, 64)
		if err != nil {
			continue
		}
		out[head[2]] = fontNames{
			size:     size,
			modified: modified,
			families: append([]string{}, fields[1:]...),
		}
	}
	return out
}

// writeFontCache writes the names down for the next run. It goes to a
// neighbouring file first and is moved into place, so a run interrupted
// half-way through leaves the previous cache intact rather than a torn one.
//
// A path with a tab in it cannot be told apart from its families on the way
// back, so it is left out and read again next time.
func writeFontCache(path string, names map[string]fontNames) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(fontCacheHeader + "\n")
	for file, n := range names {
		if strings.ContainsAny(file, "\t\n") {
			continue
		}
		b.WriteString(strconv.FormatInt(n.modified, 10))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(n.size, 10))
		b.WriteByte(' ')
		b.WriteString(file)
		for _, family := range n.families {
			b.WriteByte('\t')
			b.WriteString(family)
		}
		b.WriteByte('\n')
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
