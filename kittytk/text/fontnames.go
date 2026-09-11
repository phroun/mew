package text

// Reading a font file's family names without parsing the font.
//
// Finding a font BY NAME means knowing what every font on the machine is
// called, and the only way to learn that is to open each one. Parsing a face
// to read one string is the expensive way round: the glyphs, the metrics, the
// shaping tables and the colour layers all get built to answer "what is this
// called".
//
// A font file says its name in one small table. The header names the tables
// and where each begins, `name` holds the strings, and nothing else has to be
// touched -- not read off the disk, even. That is what this reads: a few
// hundred bytes of header, then the one table, wherever in the file it sits.
//
// Reading the whole file and parsing only the name table would be most of the
// cost again, because the files are large and the table is small.
//
// The answer has to agree with what a full parse would have said, because the
// index built from it is searched with names the parsed faces are registered
// under. A test holds the two against each other for every font on the
// machine.

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"unicode/utf16"
)

// familiesIn is every family name the font file provides -- more than one when
// the file is a collection.
func familiesIn(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	offsets, err := fontOffsets(f)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, at := range offsets {
		name, err := familyAt(f, at)
		if err != nil || name == "" {
			continue // a face this reader cannot name is left out of the index
		}
		out = appendUnique(out, name)
	}
	if len(out) == 0 {
		return nil, errors.New("font: no family name")
	}
	return out, nil
}

// at reads n bytes from off, and refuses a length that would be a mistake
// rather than a font: a table directory saying it holds a million entries is a
// file to skip, not a gigabyte to allocate.
func at(r io.ReaderAt, off int64, n int) ([]byte, error) {
	const sane = 1 << 22 // no name table or directory is anywhere near this
	if off < 0 || n < 0 || n > sane {
		return nil, errors.New("font: read out of range")
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

// fontOffsets is where each font's table directory begins. A plain font has
// one at zero; a collection has a header saying where each of its fonts is.
func fontOffsets(r io.ReaderAt) ([]uint32, error) {
	head, err := at(r, 0, 12)
	if err != nil {
		return nil, err
	}
	if string(head[0:4]) != "ttcf" {
		return []uint32{0}, nil
	}
	n := binary.BigEndian.Uint32(head[8:12])
	b, err := at(r, 12, int(n)*4)
	if err != nil {
		return nil, err
	}
	offsets := make([]uint32, 0, n)
	for i := 0; i < int(n); i++ {
		offsets = append(offsets, binary.BigEndian.Uint32(b[i*4:]))
	}
	return offsets, nil
}

// familyAt reads the family name of the font whose table directory starts at
// dir. Name id 16 is the typographic family and takes precedence over 1, the
// family a four-style family is filed under: a face called "Extra Light" is
// id 1 "Something Extra Light" and id 16 "Something", and the second is what a
// person means by the family.
func familyAt(r io.ReaderAt, dir uint32) (string, error) {
	tbl, err := nameTable(r, dir)
	if err != nil {
		return "", err
	}
	if len(tbl) < 6 {
		return "", errors.New("font: name table too short")
	}
	count := int(binary.BigEndian.Uint16(tbl[2:4]))
	storage := int(binary.BigEndian.Uint16(tbl[4:6]))
	if 6+count*12 > len(tbl) {
		return "", errors.New("font: name records truncated")
	}

	best, bestRank := "", -1
	for i := 0; i < count; i++ {
		rec := tbl[6+i*12:]
		platform := binary.BigEndian.Uint16(rec[0:2])
		encoding := binary.BigEndian.Uint16(rec[2:4])
		language := binary.BigEndian.Uint16(rec[4:6])
		nameID := binary.BigEndian.Uint16(rec[6:8])
		length := int(binary.BigEndian.Uint16(rec[8:10]))
		offset := int(binary.BigEndian.Uint16(rec[10:12]))

		if nameID != 1 && nameID != 16 {
			continue
		}
		at := storage + offset
		if at < 0 || at+length > len(tbl) {
			continue
		}
		rank := nameRank(platform, encoding, language, nameID)
		if rank <= bestRank {
			continue
		}
		s := decodeName(platform, encoding, tbl[at:at+length])
		if s == "" {
			continue
		}
		best, bestRank = s, rank
	}
	if best == "" {
		return "", errors.New("font: no family name")
	}
	return best, nil
}

// nameRank scores one name record, so the best of several wins. The
// typographic family outranks the filing family; an English record outranks
// one in a language this cannot read; and a record this can decode outranks
// one it would guess at.
func nameRank(platform, encoding, language, nameID uint16) int {
	rank := 0
	if nameID == 16 {
		rank += 8
	}
	switch platform {
	case 3: // Windows, UTF-16BE
		rank += 4
		if language == 0x0409 { // English (United States)
			rank += 2
		}
	case 0: // Unicode, UTF-16BE
		rank += 3
	case 1: // Macintosh, one byte per character
		rank += 1
		if language == 0 { // English
			rank += 2
		}
	}
	return rank
}

// decodeName turns one name record's bytes into a string. Everything but the
// Macintosh platform is UTF-16 big-endian; Macintosh is a single byte per
// character, and for a family name -- which is ASCII in all but a handful of
// fonts -- reading it as Latin-1 is right where it matters and never fails.
func decodeName(platform, encoding uint16, b []byte) string {
	if platform == 1 {
		out := make([]rune, 0, len(b))
		for _, c := range b {
			out = append(out, rune(c))
		}
		return trimNUL(string(out))
	}
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, binary.BigEndian.Uint16(b[i:i+2]))
	}
	return trimNUL(string(utf16.Decode(units)))
}

func trimNUL(s string) string {
	for len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return s
}

// nameTable finds the `name` table of the font whose table directory starts at
// dir. The directory says how many tables there are and where each one is; the
// rest of the file is never touched.
func nameTable(r io.ReaderAt, dir uint32) ([]byte, error) {
	head, err := at(r, int64(dir), 12)
	if err != nil {
		return nil, err
	}
	numTables := int(binary.BigEndian.Uint16(head[4:6]))
	records, err := at(r, int64(dir)+12, numTables*16)
	if err != nil {
		return nil, err
	}
	for i := 0; i < numTables; i++ {
		rec := records[i*16:]
		if string(rec[0:4]) != "name" {
			continue
		}
		off := int64(binary.BigEndian.Uint32(rec[8:12]))
		length := int(binary.BigEndian.Uint32(rec[12:16]))
		return at(r, off, length)
	}
	return nil, errors.New("font: no name table")
}
