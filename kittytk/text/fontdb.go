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
	must(db.register("Noto Serif", Aspect{}, fonts.SerifRegular))
	must(db.register("Noto Serif", Aspect{Bold: true}, fonts.SerifBold))
	must(db.register("Noto Serif", Aspect{Italic: true}, fonts.SerifItalic))
	must(db.register("Noto Serif", Aspect{Bold: true, Italic: true}, fonts.SerifBoldItalic))
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
	db.installUIAliases()
	// The TUI-era font names keep meaning on the graphical side:
	// Monday has always been the monospace cell font.
	db.aliases[canonical("Monday")] = []string{canonical("Noto Sans Mono")}
	db.aliases[canonical("Tuesday")] = []string{canonical("Noto Sans")}
	// "ui-fraktur" (font slot 20) is defined but unmapped, so it falls back to
	// the mono default until a Fraktur face is registered.
	db.aliases[canonical("ui-fraktur")] = []string{canonical("Noto Sans Mono")}
	return db
}

// installUIAliases wires the systematic UI font tree. Two roots — ui-text
// (KittyTK's proportional face) and ui-term (the terminal grid / purfecterm
// slot 0) — each cross four script classes (western/hebrew/arabic/cjk) and two
// styles (sans/serif). Every level is a redefinable alias with a cascading
// default, so a host or user overrides at exactly the level they care about
// (a single leaf, a whole script, or a whole root) via [window] ui_* or
// set_font. Renderers/trinkets select a face by NAME — "ui-text" (default),
// "ui-text-sans", or "ui-text-serif" — a serif/sans/default tristate that
// needs no font knowledge; the per-glyph script fallback then follows the same
// root+style (see fallbackMap). Arabic maps sans->Kufi (geometric),
// serif->Naskh (traditional), per the script's own style analogy.
func (db *fontDB) installUIAliases() {
	set := func(alias string, target string) {
		db.aliases[canonical(alias)] = []string{canonical(target)}
	}
	// Leaves: script+style -> concrete embedded family.
	leaves := map[string]string{
		"western-sans": "Noto Sans", "western-serif": "Noto Serif",
		"hebrew-sans": "Noto Sans Hebrew", "hebrew-serif": "Noto Serif Hebrew",
		"arabic-sans": "Noto Kufi Arabic", "arabic-serif": "Noto Naskh Arabic",
		"cjk-sans": "Noto Sans CJK SC", "cjk-serif": "Noto Serif CJK SC",
	}
	// The monospace root swaps the western SANS leaf for the mono face; the
	// script faces are shared (there is no monospaced Hebrew/Arabic/CJK Noto).
	// No libre "Noto Serif Mono" exists, so ui-term-western-serif uses the
	// proportional Noto Serif — the terminal grid still places each glyph in a
	// fixed cell, so it reads as a serif terminal face.
	termWestern := map[string]string{
		"western-sans": "Noto Sans Mono", "western-serif": "Noto Serif",
	}
	// Each script's default STYLE when a caller doesn't specify one.
	defStyle := map[string]string{
		"western": "sans", "hebrew": "serif", "arabic": "serif", "cjk": "sans",
	}
	for _, root := range []string{"ui-text", "ui-term"} {
		for scr, ds := range defStyle {
			for _, st := range []string{"sans", "serif"} {
				fam := leaves[scr+"-"+st]
				if root == "ui-term" {
					if tf, ok := termWestern[scr+"-"+st]; ok {
						fam = tf
					}
				}
				set(root+"-"+scr+"-"+st, fam) // e.g. ui-text-hebrew-serif -> Noto Serif Hebrew
			}
			set(root+"-"+scr, root+"-"+scr+"-"+ds) // ui-text-hebrew -> ui-text-hebrew-serif
		}
		// Style conveniences default the script to western (the primary face).
		set(root+"-sans", root+"-western-sans")
		set(root+"-serif", root+"-western-serif")
		set(root, root+"-sans") // ui-text -> ui-text-sans (sans by default)
	}
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
	// scriptRoot / scriptStyle carry the primary request's UI context ("ui-text"
	// or "ui-term"; "sans"/"serif"/"") so an uncovered Hebrew/Arabic/CJK glyph
	// falls back to the MATCHING ui-<root>-<script>-<style> face — a serif
	// primary pulls serif script faces, a term primary pulls the term variants.
	// Derived by scriptContext; empty root defaults to ui-term.
	scriptRoot  string
	scriptStyle string
}

// fallbackFor builds the fallbackMap for a font request: its resolved primary
// face plus the root/style context that steers per-glyph script fallback.
func (db *fontDB) fallbackFor(f *core.Font) fallbackMap {
	root, style := scriptContext(f.Name)
	return fallbackMap{db: db, primary: db.resolve(f), scriptRoot: root, scriptStyle: style}
}

func (m fallbackMap) ResolveFace(r rune) *gtfont.Face {
	if _, ok := m.primary.NominalGlyph(r); ok {
		return m.primary
	}
	m.db.mu.RLock()
	defer m.db.mu.RUnlock()
	// Script-class preference: for a Hebrew/Arabic/CJK rune, try the matching
	// ui-<root>-<script>[-<style>] alias's family before the general
	// registration-order chain, so the configured (or embedded default) script
	// face for this context wins even if an earlier-registered family covers r.
	if alias := m.scriptTarget(r); alias != "" {
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

// scriptTarget builds the ui-<root>-<script>[-<style>] alias to consult for a
// rune, from the class of its script and the primary's root/style context. "" when
// the rune has no non-western class (western is the primary's own job).
func (m fallbackMap) scriptTarget(r rune) string {
	cls := scriptClass(r)
	if cls == "" {
		return ""
	}
	root := m.scriptRoot
	if root == "" {
		root = "ui-term"
	}
	if m.scriptStyle != "" {
		return root + "-" + cls + "-" + m.scriptStyle // ui-text-hebrew-serif
	}
	return root + "-" + cls // ui-text-hebrew (its own default style)
}

// scriptContext derives the (root, style) a UI font name selects — root
// "ui-text"/"ui-term", style "sans"/"serif"/"" — for the per-glyph script
// fallback. A non-ui or concrete family yields ("",""), which scriptTarget
// treats as the ui-term default.
func scriptContext(name string) (root, style string) {
	n := canonical(name)
	switch {
	case strings.HasPrefix(n, "ui-text"):
		root = "ui-text"
	case strings.HasPrefix(n, "ui-term"):
		root = "ui-term"
	default:
		return "", ""
	}
	switch {
	case strings.HasSuffix(n, "-serif"):
		style = "serif"
	case strings.HasSuffix(n, "-sans"):
		style = "sans"
	}
	return root, style
}

// scriptClass classifies a rune into "hebrew", "arabic", "cjk", or "" (western
// and everything else, which the primary/general chain handles). Ranges cover
// the letters plus the Presentation Forms the RTL shapers emit.
func scriptClass(r rune) string {
	switch {
	case (r >= 0x0590 && r <= 0x05FF) || // Hebrew
		(r >= 0xFB1D && r <= 0xFB4F): // Hebrew presentation forms
		return "hebrew"
	case (r >= 0x0600 && r <= 0x06FF) || // Arabic
		(r >= 0x0750 && r <= 0x077F) || // Arabic Supplement
		(r >= 0x08A0 && r <= 0x08FF) || // Arabic Extended-A
		(r >= 0xFB50 && r <= 0xFDFF) || // Arabic Presentation Forms-A
		(r >= 0xFE70 && r <= 0xFEFF): // Arabic Presentation Forms-B
		return "arabic"
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
		return "cjk"
	}
	return ""
}
