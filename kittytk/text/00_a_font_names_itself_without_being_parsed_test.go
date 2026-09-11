package text

// Reading a font's family out of its name table, and not out of the font.
//
// The answer has to be the same one a full parse gives, because the index built
// from these names is searched with the names parsed faces are registered
// under. A family spelled differently here is a font the user asked for by name
// and did not get.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"testing"

	gtfont "github.com/go-text/typesetting/font"
)

// Every font the toolkit carries names itself the same way to both readers.
func TestTheNameTableSaysWhatTheParsedFontSays(t *testing.T) {
	files := fontFilesIn(t, "fonts")
	if len(files) == 0 {
		t.Fatal("no font files to compare the two readers on")
	}
	for _, path := range files {
		faces, err := loadFaces(path)
		if err != nil {
			t.Errorf("%s: parsing it: %v", path, err)
			continue
		}
		var parsed []string
		for _, f := range faces {
			parsed = appendUnique(parsed, f.Describe().Family)
		}
		got, err := familiesIn(path)
		if err != nil {
			t.Errorf("%s: reading its name table: %v", path, err)
			continue
		}
		if !sameNames(got, parsed) {
			t.Errorf("%s: name table says %q, the parsed font says %q", path, got, parsed)
		}
	}
}

// Nothing this cannot read is mistaken for a font, however it is malformed.
func TestSomethingThatIsNotAFontIsRefused(t *testing.T) {
	font, err := os.ReadFile(filepath.Join("fonts", "NotoSans-Regular.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	collection := append([]byte("ttcf"), 0, 1, 0, 0, 0xff, 0xff, 0xff, 0xff)

	for _, c := range []struct {
		what  string
		bytes []byte
	}{
		{"nothing at all", nil},
		{"a text file", []byte("this is not a font, it is a sentence")},
		{"a font cut short", font[:200]},
		{"a collection claiming four billion fonts in it", collection},
	} {
		path := filepath.Join(t.TempDir(), "Doubtful.ttf")
		if err := os.WriteFile(path, c.bytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := familiesIn(path); err == nil {
			t.Errorf("%s was named %q, want an error", c.what, got)
		}
	}
}

// A font that files its four styles under one name and its own family under
// another is known by the second: name id 16 is what a person means by the
// family, and id 1 is where the styled face is filed.
func TestTheTypographicFamilyIsTheOneMeant(t *testing.T) {
	path := writeFont(t, nameRec{3, 1, 0x0409, 1, "Vendor Sans Extra Light"},
		nameRec{3, 1, 0x0409, 16, "Vendor Sans"})
	if got := onlyFamily(t, path); got != "Vendor Sans" {
		t.Errorf("family is %q, want the typographic family %q", got, "Vendor Sans")
	}
}

// A font that says its name in several languages is known by the English one,
// which is the only one this can be sure it read correctly.
func TestTheEnglishNameIsTheOneTaken(t *testing.T) {
	path := writeFont(t, nameRec{3, 1, 0x040c, 1, "Police Vendeur"},
		nameRec{3, 1, 0x0409, 1, "Vendor Sans"})
	if got := onlyFamily(t, path); got != "Vendor Sans" {
		t.Errorf("family is %q, want the English name %q", got, "Vendor Sans")
	}
}

// A Macintosh record holds one byte per character where the rest hold two, and
// is read as such when it is all a font offers.
func TestAMacintoshNameIsNotReadAsPairsOfBytes(t *testing.T) {
	path := writeFont(t, nameRec{1, 0, 0, 1, "Vendor Sans"})
	if got := onlyFamily(t, path); got != "Vendor Sans" {
		t.Errorf("family is %q, want %q", got, "Vendor Sans")
	}
}

// A collection is several fonts in one file, and every family in it is found.
func TestEveryFontInACollectionIsNamed(t *testing.T) {
	base := uint32(12 + 2*4) // past the collection header and its two offsets
	first := sfntAt(base, []nameRec{{3, 1, 0x0409, 1, "Vendor Sans"}})
	second := sfntAt(base+uint32(len(first)), []nameRec{{3, 1, 0x0409, 1, "Vendor Serif"}})

	head := append([]byte("ttcf"), 0x00, 0x01, 0x00, 0x00)
	head = binary.BigEndian.AppendUint32(head, 2)
	head = binary.BigEndian.AppendUint32(head, base)
	head = binary.BigEndian.AppendUint32(head, base+uint32(len(first)))

	path := filepath.Join(t.TempDir(), "Vendor.ttc")
	whole := append(append(head, first...), second...)
	if err := os.WriteFile(path, whole, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := familiesIn(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameNames(got, []string{"Vendor Sans", "Vendor Serif"}) {
		t.Errorf("collection names %q, want both families", got)
	}
}

// fontFilesIn is every font file directly in dir.
func fontFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".ttf", ".otf", ".ttc", ".otc":
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// sameNames compares two family lists as sets: the order the two readers report
// families in is not part of the answer.
func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if gtfont.NormalizeFamily(x[i]) != gtfont.NormalizeFamily(y[i]) {
			return false
		}
	}
	return true
}

// onlyFamily is the one family the font at path provides.
func onlyFamily(t *testing.T, path string) string {
	t.Helper()
	got, err := familiesIn(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("font names %q, want one family", got)
	}
	return got[0]
}

// nameRec is one record of a name table: who it is written for, in what
// language, which of the names it is, and the text itself.
type nameRec struct {
	platform, encoding, language, id uint16
	text                             string
}

// writeFont writes the smallest file this reader will read -- a table directory
// with one `name` table in it -- and returns its path. Nothing else about a
// font is needed to say what it is called, which is the point of reading it
// this way rather than parsing it.
func writeFont(t *testing.T, recs ...nameRec) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Vendor.ttf")
	if err := os.WriteFile(path, sfntAt(0, recs), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// sfntAt builds one font whose table directory sits at base in the file it will
// be written into, which is how a collection holds more than one.
func sfntAt(base uint32, recs []nameRec) []byte {
	table := nameTableBytes(recs)
	var b []byte
	b = binary.BigEndian.AppendUint32(b, 0x00010000) // version
	b = binary.BigEndian.AppendUint16(b, 1)          // one table
	b = binary.BigEndian.AppendUint16(b, 16)         // search range
	b = binary.BigEndian.AppendUint16(b, 0)          // entry selector
	b = binary.BigEndian.AppendUint16(b, 0)          // range shift
	b = append(b, "name"...)
	b = binary.BigEndian.AppendUint32(b, 0) // checksum
	b = binary.BigEndian.AppendUint32(b, base+28)
	b = binary.BigEndian.AppendUint32(b, uint32(len(table)))
	return append(b, table...)
}

// nameTableBytes builds a name table holding recs: a header, one twelve-byte record
// each, then the strings they point into.
func nameTableBytes(recs []nameRec) []byte {
	var body, storage []byte
	for _, r := range recs {
		text := encodeFor(r.platform, r.text)
		for _, v := range []uint16{r.platform, r.encoding, r.language, r.id,
			uint16(len(text)), uint16(len(storage))} {
			body = binary.BigEndian.AppendUint16(body, v)
		}
		storage = append(storage, text...)
	}
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0) // format
	b = binary.BigEndian.AppendUint16(b, uint16(len(recs)))
	b = binary.BigEndian.AppendUint16(b, uint16(6+12*len(recs)))
	return append(append(b, body...), storage...)
}

// encodeFor writes a name the way its platform writes names: one byte per
// character for Macintosh, two for everyone else.
func encodeFor(platform uint16, s string) []byte {
	if platform == 1 {
		return []byte(s)
	}
	var b []byte
	for _, r := range s {
		b = binary.BigEndian.AppendUint16(b, uint16(r))
	}
	return b
}
