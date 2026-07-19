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
	families map[string]*family // canonical (lower-case) name -> family
	order    []string           // registration order = fallback order
	aliases  map[string]string  // canonical alias -> canonical name
	def      string             // default family (canonical)
}

func canonical(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func newFontDB() *fontDB {
	db := &fontDB{
		families: map[string]*family{},
		aliases:  map[string]string{},
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
	db.aliases[canonical("ui-text")] = canonical("Noto Sans")
	// The TUI-era font names keep meaning on the graphical side:
	// Monday has always been the monospace cell font.
	db.aliases[canonical("Monday")] = canonical("Noto Sans Mono")
	db.aliases[canonical("Tuesday")] = canonical("Noto Sans")
	return db
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
	key := canonical(name)
	if target, ok := db.aliases[key]; ok {
		key = target
	}
	fam, ok := db.families[key]
	if !ok {
		fam = db.families[db.def]
	}
	return fam.face(aspect)
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
