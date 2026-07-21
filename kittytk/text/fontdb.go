package text

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/text/fonts"
)

// Aspect selects a style variant within a family.
type Aspect struct {
	Bold   bool
	Italic bool
}

// family is one registered font family: up to four style variants,
// falling back to regular when a variant is missing.
type family struct {
	name     string
	variants map[Aspect]*gtfont.Face
}

func (f *family) face(a Aspect) *gtfont.Face {
	if face, ok := f.variants[a]; ok {
		return face
	}
	// Degrade gracefully: drop italic, then bold, then take regular.
	for _, try := range []Aspect{{Bold: a.Bold}, {Italic: a.Italic}, {}} {
		if face, ok := f.variants[try]; ok {
			return face
		}
	}
	// Any variant at all (registration guarantees at least one).
	for _, face := range f.variants {
		return face
	}
	return nil
}

// fontDB resolves core.Font descriptors to concrete faces and
// provides the per-rune fallback chain used during segmentation.
type fontDB struct {
	mu       sync.RWMutex
	families map[string]*family  // canonical (lower-case) name -> family
	order    []string            // registration order = fallback order
	aliases  map[string][]string // canonical alias -> ordered target families
	def      string              // default family (canonical)

	// searchPaths are extra directories (beyond the OS defaults) scanned
	// by RegisterFontByName; nameIndex is the lazily-built normalized
	// family-name -> file paths map over searchPaths + OS font dirs, dropped
	// whenever searchPaths changes.
	searchPaths []string
	nameIndex   map[string][]string
}

func canonical(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func newFontDB() *fontDB {
	db := &fontDB{
		families: map[string]*family{},
		aliases:  map[string][]string{},
	}
	// The embedded defaults: Noto Sans (UI) and Noto Sans Mono,
	// chosen for Unicode coverage. The Go families stay registered
	// after them purely as fallback faces, and remain addressable by
	// name. Registration order is the per-rune fallback order.
	must := func(err error) {
		if err != nil {
			panic(err) // embedded fonts cannot fail to parse
		}
	}
	must(db.register("Noto Sans", Aspect{}, fonts.SansRegular))
	must(db.register("Noto Sans", Aspect{Bold: true}, fonts.SansBold))
	must(db.register("Noto Sans", Aspect{Italic: true}, fonts.SansItalic))
	must(db.register("Noto Sans", Aspect{Bold: true, Italic: true}, fonts.SansBoldItalic))
	must(db.register("Noto Sans Mono", Aspect{}, fonts.MonoRegular))
	must(db.register("Noto Sans Mono", Aspect{Bold: true}, fonts.MonoBold))
	must(db.register("Noto Sans Hebrew", Aspect{}, fonts.HebrewRegular))
	must(db.register("Noto Sans Hebrew", Aspect{Bold: true}, fonts.HebrewBold))
	must(db.register("Noto Naskh Arabic", Aspect{}, fonts.ArabicRegular))
	must(db.register("Noto Naskh Arabic", Aspect{Bold: true}, fonts.ArabicBold))
	must(db.register("Noto Sans Symbols 2", Aspect{}, fonts.Symbols2Regular))
	must(db.register("Go", Aspect{}, goregular.TTF))
	must(db.register("Go", Aspect{Bold: true}, gobold.TTF))
	must(db.register("Go", Aspect{Italic: true}, goitalic.TTF))
	must(db.register("Go", Aspect{Bold: true, Italic: true}, gobolditalic.TTF))
	must(db.register("Go Mono", Aspect{}, gomono.TTF))
	must(db.register("Go Mono", Aspect{Bold: true}, gomonobold.TTF))
	must(db.register("Go Mono", Aspect{Italic: true}, gomonoitalic.TTF))
	must(db.register("Go Mono", Aspect{Bold: true, Italic: true}, gomonobolditalic.TTF))
	db.def = canonical("Noto Sans")
	// "ui-text" is the internal UI font name: each renderer maps it
	// to its own face. Here (the graphical engine) it is the default
	// proportional family; the text-based system maps it to Monday.
	db.aliases[canonical("ui-text")] = []string{canonical("Noto Sans")}
	// The TUI-era font names keep meaning on the graphical side:
	// Monday has always been the monospace cell font.
	db.aliases[canonical("Monday")] = []string{canonical("Noto Sans Mono")}
	db.aliases[canonical("Tuesday")] = []string{canonical("Noto Sans")}
	// "ui-term" is the terminal grid's face — the primary of purfecterm's font
	// slots (SGR 10). It starts pointed at the monospace default and is meant
	// to be re-pointed live via SetAlias ([fonts]/[window] ui_term). "ui-fraktur"
	// (slot 20) is defined but unmapped, so it falls back to the mono default
	// until a Fraktur face is registered.
	db.aliases[canonical("ui-term")] = []string{canonical("Noto Sans Mono")}
	db.aliases[canonical("ui-fraktur")] = []string{canonical("Noto Sans Mono")}
	return db
}

// setAlias re-points a canonical alias at an ordered list of target families
// (resolve picks the first registered one). An empty list deletes the alias.
func (db *fontDB) setAlias(alias string, targets []string) {
	key := canonical(alias)
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(targets) == 0 {
		delete(db.aliases, key)
		return
	}
	list := make([]string, len(targets))
	for i, t := range targets {
		list[i] = canonical(t)
	}
	db.aliases[key] = list
}

func (db *fontDB) register(familyName string, a Aspect, ttf []byte) error {
	face, err := gtfont.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		return fmt.Errorf("text: parsing font %q: %w", familyName, err)
	}
	db.registerFace(familyName, a, face)
	return nil
}

func (db *fontDB) registerFace(familyName string, a Aspect, face *gtfont.Face) {
	key := canonical(familyName)
	db.mu.Lock()
	defer db.mu.Unlock()
	fam, ok := db.families[key]
	if !ok {
		fam = &family{name: familyName, variants: map[Aspect]*gtfont.Face{}}
		db.families[key] = fam
		db.order = append(db.order, key)
	}
	fam.variants[a] = face
}

// resolve returns the face for a font descriptor: named family (or
// alias) if registered, the default family otherwise.
func (db *fontDB) resolve(f *core.Font) *gtfont.Face {
	name, aspect := describe(f)
	db.mu.RLock()
	defer db.mu.RUnlock()
	fk, found := db.resolveFamily(canonical(name), 0)
	if !found {
		fk = db.def
	}
	fam, ok := db.families[fk]
	if !ok {
		fam = db.families[db.def]
	}
	return fam.face(aspect)
}

// resolveFamily follows aliases — possibly nested (ui-term -> Monday -> Noto
// Sans Mono) and with fallback lists ("JetBrainsMono, Monday" uses Monday when
// the first is unregistered) — to a registered family's canonical key. found
// reports whether it landed on a real family. Bounded against alias cycles.
// Caller holds the lock.
func (db *fontDB) resolveFamily(key string, depth int) (famKey string, found bool) {
	if depth > 8 {
		return key, false
	}
	if _, ok := db.families[key]; ok {
		return key, true
	}
	targets, ok := db.aliases[key]
	if !ok {
		return key, false // neither a family nor an alias
	}
	last := key
	for _, t := range targets {
		if fk, ok := db.resolveFamily(t, depth+1); ok {
			return fk, true
		} else {
			last = fk // remember the guaranteed-fallback's landing key
		}
	}
	return last, false
}

// describe maps a core.Font to a family name + aspect.
func describe(f *core.Font) (string, Aspect) {
	if f == nil {
		f = core.DefaultFont()
	}
	return f.Name, Aspect{
		Bold:   f.HasStyle(core.FontStyleBold),
		Italic: f.HasStyle(core.FontStyleItalic),
	}
}

// fallbackMap implements shaping.Fontmap for one shaping request: the
// requested face first, then every registered family in registration
// order, per rune. Deterministic (D5: layout must not depend on the
// substrate or environment) because it consults only registered
// fonts, never the host system.
type fallbackMap struct {
	db      *fontDB
	primary *gtfont.Face
}

func (m fallbackMap) ResolveFace(r rune) *gtfont.Face {
	if _, ok := m.primary.NominalGlyph(r); ok {
		return m.primary
	}
	m.db.mu.RLock()
	defer m.db.mu.RUnlock()
	for _, key := range m.db.order {
		face := m.db.families[key].face(Aspect{})
		if face == nil {
			continue
		}
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	// Nothing covers it: the primary face renders its .notdef glyph.
	return m.primary
}
