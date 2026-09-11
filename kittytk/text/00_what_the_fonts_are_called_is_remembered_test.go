package text

// What the machine's fonts are called, kept between runs.
//
// Every test here builds the index twice over the same directory, which is what
// two starts of the program amount to. Between the two the fonts are tampered
// with in ways the cache is supposed to notice, or in ways it is supposed to be
// spared -- a file whose size and time have not moved is not opened, and the
// proof of that is that opening it would now fail.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gtfont "github.com/go-text/typesetting/font"
)

// The second start finds the family without opening the file again.
func TestASecondStartTakesTheNamesFromWhatItWroteDown(t *testing.T) {
	dir := fontDir(t)
	path := placeFont(t, dir, "Vendor.ttf", "Vendor Sans")

	if !holds(buildNameIndex([]string{dir}), "Vendor Sans", path) {
		t.Fatal("the first start did not find Vendor Sans")
	}
	if got := cacheText(t); !strings.Contains(got, path) {
		t.Fatalf("the cache does not mention %s:\n%s", path, got)
	}

	scramble(t, path)
	if !holds(buildNameIndex([]string{dir}), "Vendor Sans", path) {
		t.Error("the second start opened the file instead of trusting the cache")
	}
}

// A font that has changed since it was written down is read again, whether it
// is the size or the time that says so.
func TestAFontThatChangedIsReadAgain(t *testing.T) {
	dir := fontDir(t)
	touched := placeFont(t, dir, "Touched.ttf", "Vendor Sans")
	grown := placeFont(t, dir, "Grown.ttf", "Vendor Serif")

	index := buildNameIndex([]string{dir})
	if !holds(index, "Vendor Sans", touched) || !holds(index, "Vendor Serif", grown) {
		t.Fatal("the first start did not find both families")
	}

	// One keeps its size and is given a later time; the other keeps its time
	// and is given a different size. Either is enough to be read again.
	scramble(t, touched)
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(touched, later, later); err != nil {
		t.Fatal(err)
	}
	when := modified(t, grown)
	if err := os.WriteFile(grown, []byte("not a font any more"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(grown, when, when); err != nil {
		t.Fatal(err)
	}

	index = buildNameIndex([]string{dir})
	if holds(index, "Vendor Sans", touched) {
		t.Error("a font touched since it was written down was not read again")
	}
	if holds(index, "Vendor Serif", grown) {
		t.Error("a font that changed size was not read again")
	}
}

// A font that is no longer anywhere the search paths reach stops being
// remembered, so the cache stays the size of what the machine has.
func TestAFontThatWentAwayIsForgotten(t *testing.T) {
	dir := fontDir(t)
	kept := placeFont(t, dir, "Kept.ttf", "Vendor Sans")
	gone := placeFont(t, dir, "Gone.ttf", "Vendor Serif")
	buildNameIndex([]string{dir})

	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	index := buildNameIndex([]string{dir})
	if holds(index, "Vendor Serif", gone) {
		t.Error("a font that is not there is still in the index")
	}
	if !holds(index, "Vendor Sans", kept) {
		t.Error("the font that stayed went missing too")
	}
	if got := cacheText(t); strings.Contains(got, gone) {
		t.Errorf("the cache still mentions %s:\n%s", gone, got)
	}
}

// A start that learns nothing new leaves the cache file alone rather than
// writing the same thing over it.
func TestTheCacheIsLeftAloneWhenNothingChanged(t *testing.T) {
	dir := fontDir(t)
	path := placeFont(t, dir, "Vendor.ttf", "Vendor Sans")
	buildNameIndex([]string{dir})

	mark := "# this line is here to see whether the file was written again\n"
	if err := os.WriteFile(fontCachePath(), []byte(cacheText(t)+mark), 0o600); err != nil {
		t.Fatal(err)
	}

	if !holds(buildNameIndex([]string{dir}), "Vendor Sans", path) {
		t.Fatal("the second start did not find Vendor Sans")
	}
	if got := cacheText(t); !strings.Contains(got, mark) {
		t.Errorf("the cache was written again with nothing new to say:\n%s", got)
	}
}

// A path with a tab in it cannot be told apart from the families written after
// it, so it is left out of the cache and read again every start.
func TestAFontWhosePathHasATabInItIsNotWrittenDown(t *testing.T) {
	dir := fontDir(t)
	path := placeFont(t, dir, "Ven\tdor.ttf", "Vendor Sans")

	if !holds(buildNameIndex([]string{dir}), "Vendor Sans", path) {
		t.Fatal("the first start did not find Vendor Sans")
	}
	if got := cacheText(t); strings.Contains(got, "Ven\tdor.ttf") {
		t.Errorf("a path with a tab in it was written to the cache:\n%q", got)
	}
	if !holds(buildNameIndex([]string{dir}), "Vendor Sans", path) {
		t.Error("the second start lost a font it cannot write down")
	}
}

// fontDir is a directory to put fonts in, with the cache pointed somewhere
// throwaway so the suite never touches the one this machine's user has.
func fontDir(t *testing.T) string {
	t.Helper()
	t.Setenv(FontCacheEnv, filepath.Join(t.TempDir(), "cache", "fonts", "index"))
	return t.TempDir()
}

// placeFont writes a font named family into dir.
func placeFont(t *testing.T, dir, file, family string) string {
	t.Helper()
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, sfntAt(0, []nameRec{{3, 1, 0x0409, 1, family}}), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// holds says whether the index found family in the file at path.
func holds(index map[string][]string, family, path string) bool {
	for _, p := range index[gtfont.NormalizeFamily(family)] {
		if p == path {
			return true
		}
	}
	return false
}

// cacheText is the cache file as written.
func cacheText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(fontCachePath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// modified is when the file was last written.
func modified(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// scramble replaces a file's contents with something that is not a font while
// leaving its size and its time alone, so nothing the cache compares has moved.
// Opening it now fails; trusting what was written down does not.
func scramble(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, int(info.Size())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got, err := familiesIn(path); err == nil {
		t.Fatalf("a scrambled file still reads as a font called %q", got)
	}
}
