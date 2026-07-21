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
	// Hebrew: Serif first, so it wins the per-rune fallback (the more legible
	// body face, with the clearest niqqud). Noto Sans Hebrew stays registered
	// after it — addressable by name for a sans look — but no longer the default.
	must(db.register("Noto Serif Hebrew", Aspect{}, fonts.HebrewSerifRegular))
	must(db.register("Noto Serif Hebrew", Aspect{Bold: true}, fonts.HebrewSerifBold))
	must(db.register("Noto Sans Hebrew", Aspect{}, fonts.HebrewRegular))
	must(db.register("Noto Sans Hebrew", Aspect{Bold: true}, fonts.HebrewBold))
	// Arabic: Naskh first, so it wins the per-rune fallback for the terminal
	// grid (the most legible body-text style). Kufi is registered too — a
	// geometric display face addressable by name for a retro look — but comes
	// after Naskh, so it never preempts the default.
	must(db.register("Noto Naskh Arabic", Aspect{}, fonts.ArabicRegular))
	must(db.register("Noto Naskh Arabic", Aspect{Bold: true}, fonts.ArabicBold))
	must(db.register("Noto Kufi Arabic", Aspect{}, fonts.ArabicKufiRegular))
	must(db.register("Noto Kufi Arabic", Aspect{Bold: true}, fonts.ArabicKufiBold))
	// Pan-CJK: full C/J/K coverage in one face each. Sans is the default CJK
	// fallback; the serif (byte-identical to Noto Serif CJK SC) is by name.
	// Register the sans first so it wins the per-rune fallback for CJK.
	must(db.register("Noto Sans CJK SC", Aspect{}, fonts.CJKSansRegular))
	must(db.register("Noto Serif CJK SC", Aspect{}, fonts.CJKSerifRegular))
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
	// Script-class defaults for the terminal grid: when the primary (ui-term)
	// doesn't cover a glyph, resolution consults ui-term-<class> for the rune's
	// script BEFORE the general fallback chain (see scriptClassAlias /
	// fallbackMap.ResolveFace). Each is a redefinable alias ([window]
	// ui_term_cjk/ui_term_hebrew/ui_term_arabic, or set_font), so a host can
	// pick its own script faces; unset, it falls through to the general chain.
	db.aliases[canonical("ui-term-cjk")] = []string{canonical("Noto Sans CJK SC")}
	db.aliases[canonical("ui-term-hebrew")] = []string{canonical("Noto Serif Hebrew")}
	db.aliases[canonical("ui-term-arabic")] = []string{canonical("Noto Naskh Arabic")}
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
	// Script-class preference: for a Hebrew/Arabic/CJK rune, try the
	// ui-term-<class> alias's family before the general registration-order
	// chain, so a host's chosen (or the embedded default) script face wins even
	// if some earlier-registered family happens to also cover the rune.
	if alias := scriptClassAlias(r); alias != "" {
		if fk, ok := m.db.resolveFamily(alias, 0); ok {
			if fam := m.db.families[fk]; fam != nil {
				if face := fam.face(Aspect{}); face != nil {
					if _, ok := face.NominalGlyph(r); ok {
						return face
					}
				}
			}
		}
	}
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

// scriptClassAlias returns the canonical ui-term-<class> alias for a rune's
// script — "ui-term-hebrew", "ui-term-arabic", or "ui-term-cjk" — or "" for
// scripts with no class (Latin and the rest resolve via the general chain).
// Ranges cover the letters plus the Presentation Forms the RTL shapers emit.
func scriptClassAlias(r rune) string {
	switch {
	case (r >= 0x0590 && r <= 0x05FF) || // Hebrew
		(r >= 0xFB1D && r <= 0xFB4F): // Hebrew presentation forms
		return "ui-term-hebrew"
	case (r >= 0x0600 && r <= 0x06FF) || // Arabic
		(r >= 0x0750 && r <= 0x077F) || // Arabic Supplement
		(r >= 0x08A0 && r <= 0x08FF) || // Arabic Extended-A
		(r >= 0xFB50 && r <= 0xFDFF) || // Arabic Presentation Forms-A
		(r >= 0xFE70 && r <= 0xFEFF): // Arabic Presentation Forms-B
		return "ui-term-arabic"
	case (r >= 0x1100 && r <= 0x11FF) || // Hangul Jamo
		(r >= 0x3040 && r <= 0x30FF) || // Hiragana + Katakana
		(r >= 0x3100 && r <= 0x312F) || // Bopomofo
		(r >= 0x3130 && r <= 0x318F) || // Hangul Compatibility Jamo
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Ext A
		(r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0xA960 && r <= 0xA97F) || // Hangul Jamo Extended-A
		(r >= 0xAC00 && r <= 0xD7FF) || // Hangul Syllables + Jamo Ext-B
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0xFF00 && r <= 0xFFEF) || // Halfwidth/Fullwidth Forms
		(r >= 0x20000 && r <= 0x2FA1F): // CJK Ext B..F + compat supplement
		return "ui-term-cjk"
	}
	return ""
}
