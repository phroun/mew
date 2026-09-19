package display

// Building a data source out of what a bundle declares.
//
// This is the seam. A bundle is an authored DOCUMENT held in the store; a
// source is a live thing with scopes, caches and invalidation. Loading is what
// turns one into the other, and it is the only place that knows both.
//
// What a bundle declares, and what it becomes:
//
//	includes      →  a ComposedSource, every record under its include's name
//	_amendments   →  an AmendedSource over the whole of that
//	its records   →  records of the amended layer, under keys of their own
//
// So a layer stack is a composition with an amendment over it, which is why
// there is no third merge here and no precedence to decide: nothing shadows by
// position, and what shadows is an amendment naming one record.
//
// **Sharing is by selector.** Two includes wanting the newest figaro get the
// SAME source object, so every cache and every invalidation over it is shared.
// Resolution happens once per selector per load, which also means two includes
// cannot disagree about what "newest" was because only one of them ever asked.
//
// A bundle that includes nothing is its records and no more -- no composition
// to merge and no amendment layer to carry, which is the leaf case and the
// common one.
//
// An include names a BUNDLE or a SOURCE, and they are two namespaces. A bundle
// is a document in the store, found by key and version. A source is a live
// thing somebody registered under a name -- an application's records, a CSV
// read at run time, anything at all -- and it has no version to select between,
// because it is one object rather than a shelf of them. That is what lets a
// bundle wrap and amend a source of ANY kind rather than only other bundles.

import (
	"fmt"
	"sort"

	"github.com/phroun/pawscript"
	"github.com/phroun/serval"
)

// amendmentsMark is the member holding what a bundle changes about what it
// includes. Like `_bundle` it is about the document rather than in it, so it is
// not one of the records.
const amendmentsMark = "_amendments"

// A Trouble is one thing that was wrong with a load.
//
// It is a report, not a refusal. An optional include that resolves to nothing
// is absent and said so; a cycle has its back edge dropped and said so; the
// load finishes either way. Only a MANDATORY include that cannot be met stops
// it, because an author said that one had to be there.
type Trouble struct {
	Bundle string // the bundle the trouble was in, by key
	Alias  string // the include it was about, where it was about one
	Reason string
}

func (t Trouble) String() string {
	where := t.Bundle
	if t.Alias != "" {
		where += "/" + t.Alias
	}
	return where + ": " + t.Reason
}

// A Loaded bundle: the source it became, and everything that went wrong on the
// way that did not stop it.
type Loaded struct {
	Source  serval.Source
	Trouble []Trouble
}

// loader is one load in progress. It lives no longer than the load, because
// what it holds -- which selector resolved to what -- is settled at the moment
// of loading and settled again, possibly differently, at the next.
type loader struct {
	store   *appStore
	live    SourceSet
	built   map[string]serval.Source // by selector, which is what sharing is by
	open    map[string]bool          // the bundles above this one, for cycles
	trouble []Trouble
}

// A SourceSet is where a bundle's live includes are looked up.
//
// **It ANSWERS for a name rather than holding one**, which a plain map cannot. A
// connection can stand up the far end of a name its application hosts, and it can
// only do that when somebody asks -- a bundle's include being exactly that
// somebody, and the ask arriving in the middle of a load rather than before it.
type SourceSet interface {
	// Source is what a name stands for here, and false for one that stands for
	// nothing.
	Source(name string) (serval.Source, bool)
}

// Sources are live sources registered by name, for an include to reach with
// `source:`. They are the caller's, made before the load and outliving it.
type Sources map[string]serval.Source

// Source is what a name stands for in this set (SourceSet). A map answers only
// for what it holds, which is every caller that knows its names up front.
func (s Sources) Source(name string) (serval.Source, bool) {
	src, held := s[name]
	return src, held && src != nil
}

// LoadBundle builds a source out of the bundle a store holds under a key and a
// version, following what it includes.
func (s *appStore) LoadBundle(key, version string, live SourceSet) (*Loaded, error) {
	l := &loader{
		store: s,
		live:  live,
		built: map[string]serval.Source{},
		open:  map[string]bool{},
	}
	src, err := l.build(want{selector: selector{key: key, pin: version}})
	if err != nil {
		return nil, err
	}
	return &Loaded{Source: src, Trouble: l.trouble}, nil
}

// build is the source a want resolves to, made once per selector and handed out
// again to everything else wanting the same.
func (l *loader) build(w want) (serval.Source, error) {
	at := w.selector.String()
	if src, made := l.built[at]; made {
		return src, nil
	}
	if w.live {
		// A registered source is already a source. There is nothing to read,
		// nothing to assemble, and no ring to close: it was made before this
		// load began.
		if l.live == nil {
			return nil, fmt.Errorf("nothing is registered as a source called %q", w.key)
		}
		src, held := l.live.Source(w.key)
		if !held || src == nil {
			return nil, fmt.Errorf("nothing is registered as a source called %q", w.key)
		}
		l.built[at] = src
		return src, nil
	}
	if l.open[at] {
		return nil, fmt.Errorf("%s includes itself", at)
	}

	entry, text, err := l.resolve(w)
	if err != nil {
		return nil, err
	}

	l.open[at] = true
	defer delete(l.open, at)

	src, err := l.assemble(entry, text)
	if err != nil {
		return nil, err
	}
	l.built[at] = src
	return src, nil
}

// resolve finds the item a want names, and reads it.
//
// Newest-wins: every candidate under the key is ordered and the highest taken,
// and the want's floor is a question about THAT rather than about which to
// take. So an include asking for a floor it cannot have is told what it got and
// why that is not enough, rather than quietly being given something older.
func (l *loader) resolve(w want) (bundleEntry, string, error) {
	if w.hashWanted() {
		hit := l.store.bundlesHashed(w.pin)
		if len(hit) == 0 {
			return bundleEntry{}, "", fmt.Errorf("no bundle here hashes to %s", w.pin)
		}
		if len(hit) > 1 {
			return bundleEntry{}, "", fmt.Errorf("%d items hash to %s", len(hit), w.pin)
		}
		text, err := l.read(hit[0])
		return hit[0], text, err
	}
	if w.pin != "" {
		hit := l.store.bundlesNamed(w.key, w.pin)
		if len(hit) == 0 {
			return bundleEntry{}, "", fmt.Errorf("no bundle here is %s at %s", w.key, w.pin)
		}
		if len(hit) > 1 {
			return bundleEntry{}, "", fmt.Errorf(
				"%d items claim to be %s at %s, so which is meant is the author's to settle",
				len(hit), w.key, w.pin)
		}
		text, err := l.read(hit[0])
		return hit[0], text, err
	}

	best, held := l.newest(w)
	if !held {
		return bundleEntry{}, "", fmt.Errorf("no bundle here is called %s", w.key)
	}
	if !w.admits(best.version) {
		return bundleEntry{}, "", fmt.Errorf(
			"the newest %s here is %s, which is not one %s will take",
			w.key, best.version, w.String())
	}
	text, err := l.read(best)
	return best, text, err
}

// newest is the highest version under a want's key that its UPPER bound admits.
// The floor takes no part: it is checked on the answer, which is what lets two
// includes with different floors share one selector.
func (l *loader) newest(w want) (bundleEntry, bool) {
	var best bundleEntry
	var held bool
	for _, e := range l.store.bundlesUnder(w.key) {
		if w.upper != "" && compareVersions(e.version, w.upper) >= 0 {
			continue
		}
		if !held || compareVersions(e.version, best.version) > 0 {
			best, held = e, true
		}
	}
	return best, held
}

// read is a bundle's text, whole.
func (l *loader) read(e bundleEntry) (string, error) {
	var out []byte
	for offset := int64(0); ; {
		data, _, last, err := l.store.read(e.storeKey, offset)
		if err != nil {
			return "", err
		}
		out = append(out, data...)
		offset += int64(len(data))
		if last {
			return string(out), nil
		}
		if len(data) == 0 {
			return "", fmt.Errorf("%q would not read to its end", e.storeKey)
		}
	}
}

// assemble is what a bundle's text becomes.
func (l *loader) assemble(e bundleEntry, text string) (serval.Source, error) {
	n, err := pawscript.ParsePSL(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.key, err)
	}
	// The bundle's own records are the document less what is ABOUT it.
	own := serval.NewPSLSourceExcept(n, serval.Whole, bundleMark, amendmentsMark)

	includes, err := l.includesOf(e, n)
	if err != nil {
		return nil, err
	}
	amendments := amendmentsOf(n)
	if len(includes) == 0 {
		if len(amendments) > 0 {
			l.note(e.key, "", "this amends what it includes, and it includes nothing")
		}
		l.sayShape(e, n, own)
		return own, nil // a leaf: its records, and no layer to carry
	}

	composed, err := serval.NewComposedSource(includes...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.key, err)
	}
	over := serval.NewAmendedSource(composed)

	// Its own records stand beside what it included, under keys of their own.
	set, err := own.Open(&serval.Spec{})
	if err != nil {
		return nil, err
	}
	rows := &collect{}
	if err := set.Read(&serval.Scope{Count: own.Len()}, rows); err != nil {
		set.Close()
		return nil, err
	}
	set.Close()
	for i, id := range rows.ids {
		over.Add(id, rows.fields[i])
	}

	for _, a := range amendments {
		if a.gone {
			over.Delete(a.key, nil)
		} else {
			over.Replace(a.key, a.fields)
		}
	}
	// Said of the whole assembled layer, which is what a reader is handed. An
	// include's own hint stays with the include -- see bundletree.go.
	l.sayShape(e, n, over)
	return over, nil
}

// includesOf resolves what a bundle includes, in the order it named them.
func (l *loader) includesOf(e bundleEntry, n *pawscript.PSLNode) ([]serval.Include, error) {
	meta, ok := n.Get(bundleMark)
	if !ok {
		return nil, nil
	}
	block, ok := meta.(*pawscript.PSLNode)
	if !ok {
		return nil, nil
	}
	held, ok := block.Get("includes")
	if !ok {
		return nil, nil
	}
	list, ok := held.(*pawscript.PSLNode)
	if !ok {
		return nil, nil
	}

	aliases := make([]string, 0, len(list.Map()))
	for alias := range list.Map() {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	var out []serval.Include
	for _, alias := range aliases {
		w, err := wantOf(list, alias)
		if err != nil {
			l.note(e.key, alias, err.Error())
			continue
		}
		src, err := l.build(w)
		if err != nil {
			if w.optional {
				l.note(e.key, alias, err.Error()+", and this include is optional")
				continue
			}
			return nil, fmt.Errorf("%s/%s: %w", e.key, alias, err)
		}
		out = append(out, serval.Include{Name: alias, Source: src})
	}
	return out, nil
}

// wantOf reads one include.
//
// The ALIAS is the bundle's key, which is what makes the terse form terse:
// `figaro: ">= 0.1.0"` says what to include and what to call it in one word,
// and that is the common case. Where the two need to differ -- two versions of
// one bundle side by side, or a local name that reads better -- a list says the
// key outright and the alias is only the name it goes under here.
func wantOf(list *pawscript.PSLNode, alias string) (want, error) {
	v, _ := list.Get(alias)
	switch x := v.(type) {
	case string:
		return parseWant(alias, x)
	case pawscript.Symbol:
		return parseWant(alias, string(x))
	case *pawscript.PSLNode:
		w, err := listWant(alias, x)
		if err != nil {
			return want{}, err
		}
		if b, held := x.Get(optionalTerm); held {
			if on, ok := b.(bool); ok {
				w.optional = on
			}
		}
		return w, nil
	}
	return want{}, fmt.Errorf("this include is bound to nothing a version is read from")
}

// listWant reads the long form, which is where an include says WHICH namespace
// it means.
//
//	( bundle: "drop-folder", want: ">= 0.1.0" )   a document in the store
//	( source: "mail.incoming" )                   a source registered by name
//
// A registered source takes no version expression. There is one of it, so there
// is nothing to select between, and a `want` on one is refused rather than
// quietly ignored -- an author who wrote it believed something that is not so.
func listWant(alias string, x *pawscript.PSLNode) (want, error) {
	named := pslText(x, "source")
	held := pslText(x, "bundle")
	switch {
	case named != "" && held != "":
		return want{}, fmt.Errorf("this include names both a source and a bundle")
	case named != "":
		if pslText(x, "want") != "" {
			return want{}, fmt.Errorf(
				"%q is a registered source, and there is one of it: a version says nothing about which", named)
		}
		return want{selector: selector{key: named, live: true}}, nil
	}
	if held == "" {
		held = alias // the terse rule: an alias is the bundle's key
	}
	return parseWant(held, pslText(x, "want"))
}

// An amendment as the document states it: a record replaced, or one deleted.
type amendment struct {
	key    *serval.Value
	fields serval.Record
	gone   bool
}

// amendmentsOf reads `_amendments`. A member with nothing under it is a
// deletion; anything else stands in the record's place.
func amendmentsOf(n *pawscript.PSLNode) []amendment {
	held, ok := n.Get(amendmentsMark)
	if !ok {
		return nil
	}
	list, ok := held.(*pawscript.PSLNode)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(list.Map()))
	for k := range list.Map() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]amendment, 0, len(keys))
	for _, k := range keys {
		v, _ := list.Get(k)
		a := amendment{key: serval.NewSymbol(k)}
		if v == nil {
			a.gone = true
			out = append(out, a)
			continue
		}
		a.fields = serval.PSLFields(a.key, v, serval.Whole)
		out = append(out, a)
	}
	return out
}

func (l *loader) note(bundle, alias, reason string) {
	l.trouble = append(l.trouble, Trouble{Bundle: bundle, Alias: alias, Reason: reason})
}

// collect takes a source's records as they stand, which is how a bundle's own
// records are lifted onto the layer that carries them.
type collect struct {
	ids    []*serval.Value
	fields []serval.Record
}

func (c *collect) Ordered() {}
func (c *collect) Record(id *serval.Value, f serval.Record) error {
	c.ids = append(c.ids, id)
	c.fields = append(c.fields, f)
	return nil
}
func (c *collect) Subset(id *serval.Value, f serval.Record, _ serval.Totals) error {
	return c.Record(id, f)
}
func (c *collect) Done(serval.Complete) {}

// bundlesUnder is every bundle this store holds under one key, cached before
// kept so that what an app most recently put there is what answers.
func (s *appStore) bundlesUnder(key string) []bundleEntry {
	under := func(all []bundleEntry) []bundleEntry {
		var out []bundleEntry
		for _, e := range all {
			if e.key == key {
				out = append(out, e)
			}
		}
		return out
	}
	if hit := under(s.cached.bundles()); len(hit) > 0 {
		return hit
	}
	return under(s.kept.bundles())
}
