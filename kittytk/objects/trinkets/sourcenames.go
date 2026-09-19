package trinkets

// Naming a source from the wire.
//
// A trinket written down in the KittyTK Wire Language says what it reads by
// NAME, because a statement cannot carry a Go value and the thing it names may
// not exist yet when the statement is read.
//
// Two namespaces, and they are the two a bundle's includes already use, so there
// is one idiom rather than two:
//
//	source:mail.incoming     a live source somebody registered under that name
//	bundle:objectLibrary     a bundle, found and assembled by the loader
//	objectLibrary            the same, the bare form meaning a bundle
//
// A live SOURCE is one object, registered by whatever made it, with no version
// to choose between. A BUNDLE is a document selected by version, and turning one
// into a source means finding it, resolving its includes and assembling them --
// which needs a store, and a store belongs to the display. So bundles resolve
// through a hook the display installs, and this package holds the hook rather
// than the knowledge.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/serval"
)

// The two namespaces, spelled as a bundle's includes spell them.
const (
	sourceMark = "source:"
	bundleMark = "bundle:"
)

var sources = struct {
	mu     sync.RWMutex
	live   map[string]serval.Source
	loader func(key, version string) (serval.Source, error)
}{live: map[string]serval.Source{}}

// RegisterSource files a source under a name, so that something written down in
// the wire language can ask for it.
//
// Registering over a name replaces it. Whatever already read the old one goes on
// reading it -- a trinket holds the source it was given, not the name it was
// given it under -- so this decides what the NEXT asker gets and nothing else.
func RegisterSource(name string, src serval.Source) {
	if name == "" || src == nil {
		return
	}
	sources.mu.Lock()
	defer sources.mu.Unlock()
	sources.live[name] = src
}

// UnregisterSource drops a name. Trinkets already reading that source are
// untouched, for the same reason.
func UnregisterSource(name string) {
	sources.mu.Lock()
	defer sources.mu.Unlock()
	delete(sources.live, name)
}

// SetBundleLoader installs what turns a bundle's key and version into a source.
//
// It is a hook because assembling a bundle means reaching a STORE, and a store
// is the display's. This package holds the names; the display holds the
// knowledge. Nil takes it away again, and a `bundle:` name then says plainly
// that nothing here can find one rather than failing as though the bundle were
// missing.
func SetBundleLoader(fn func(key, version string) (serval.Source, error)) {
	sources.mu.Lock()
	defer sources.mu.Unlock()
	sources.loader = fn
}

// LookupSource is what a name stands for.
//
// A version may ride a bundle's name after an `@`, which is the spelling a
// selector already uses. No version means the newest there is, which is what an
// include with no bound means too.
func LookupSource(name string) (serval.Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("a source is named, and this names nothing")
	}

	if live, ok := strings.CutPrefix(name, sourceMark); ok {
		sources.mu.RLock()
		defer sources.mu.RUnlock()
		src := sources.live[live]
		if src == nil {
			return nil, fmt.Errorf("source %q: nothing is registered under that name", live)
		}
		return src, nil
	}

	key := strings.TrimPrefix(name, bundleMark)
	version := ""
	if at := strings.LastIndex(key, "@"); at >= 0 {
		key, version = key[:at], key[at+1:]
	}
	if key == "" {
		return nil, fmt.Errorf("bundle %q: a bundle is named by a key", name)
	}

	sources.mu.RLock()
	loader := sources.loader
	sources.mu.RUnlock()
	if loader == nil {
		return nil, fmt.Errorf("bundle %q: nothing here knows how to find a bundle;"+
			" a display installs that", key)
	}
	return loader(key, version)
}

// BundleName reads a name as a BUNDLE's: its key, and the version riding after
// an `@` where one does. False for a name that means a live source instead.
//
// It is exported because whoever can actually find a bundle is not this package
// -- assembling one means reaching a store -- so somebody else has to be able to
// tell the two kinds of name apart with the same reading this does.
func BundleName(name string) (key, version string, ok bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, sourceMark) {
		return "", "", false
	}
	key = strings.TrimPrefix(name, bundleMark)
	if at := strings.LastIndex(key, "@"); at >= 0 {
		key, version = key[:at], key[at+1:]
	}
	if key == "" {
		return "", "", false
	}
	return key, version, true
}

// LiveName reads a name as a LIVE source's: the name itself, with the `source:`
// mark off where it wore one. False for a name that means a bundle instead.
//
// It is the other half of BundleName and it is exported for the same reason:
// whoever turns a name into something is not always this package, and the two
// kinds must be told apart by one reading rather than by each caller spelling the
// mark out again.
func LiveName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if live, ok := strings.CutPrefix(name, sourceMark); ok {
		return live, live != ""
	}
	// No mark at all is a bundle's bare form, which is what BundleName takes.
	return "", false
}

// LiveSources is every name registered in this process, as a map somebody else
// can hold. A copy, so that registering later does not change one in flight.
func LiveSources() map[string]serval.Source {
	sources.mu.RLock()
	defer sources.mu.RUnlock()
	out := make(map[string]serval.Source, len(sources.live))
	for name, src := range sources.live {
		out[name] = src
	}
	return out
}

// --- one connection's own way of finding them -----------------------------

// A SourceFinder turns a name into a source for ONE connection.
//
// Connection-scoped because a STORE is: the desktop keeps a separate one per
// host identity and app name, so the bundle two different applications each call
// `objectLibrary` is two different bundles. A process-wide answer would hand one
// application another's, which is the hole the whole trust boundary exists to
// close.
type SourceFinder func(name string) (serval.Source, error)

// sourcesStash is where a connection's finder is kept. Stash is what
// connection-scoped trinket state uses, so the protocol package goes on knowing
// nothing about sources.
const sourcesStash = "trinkets.sources"

type sourceHolder struct {
	mu   sync.RWMutex
	find SourceFinder
}

func holderIn(ctx *protocol.BindContext) *sourceHolder {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Stash(sourcesStash, func() any { return &sourceHolder{} }).(*sourceHolder)
	return h
}

// SetSourceFinder gives one connection its own way of finding sources.
func SetSourceFinder(ctx *protocol.BindContext, find SourceFinder) {
	h := holderIn(ctx)
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.find = find
}

// LookupSourceOn is what a name stands for ON a connection.
//
// The connection's own finder where it has one, and the process-wide names
// where it has not -- which is what an application built in Go, with no
// connection at all, is reading.
func LookupSourceOn(ctx *protocol.BindContext, name string) (serval.Source, error) {
	if h := holderIn(ctx); h != nil {
		h.mu.RLock()
		find := h.find
		h.mu.RUnlock()
		if find != nil {
			return find(name)
		}
	}
	return LookupSource(name)
}

// SetSourceByName points this list at a named source.
//
// It is the wire's way in, and it is exported because an application in Go has
// the same reason to use a name: whatever registered the source and whatever
// reads it need not know about each other.
func (l *ListView) SetSourceByName(name string) error {
	src, err := LookupSource(name)
	if err != nil {
		return err
	}
	l.SetSource(src)
	return nil
}

// SetFields says which of a record's fields this list SHOWS and which it means.
//
// They are one field until somebody says otherwise: a plain list of strings has
// nothing to tell them apart. A list over a delimited file has -- the column it
// shows a reader and the column it hands back to the program need not be the
// same one, and a list that could only do the first would have the second put
// somewhere beside it.
//
// An empty name leaves that half alone.
func (l *ListView) SetFields(display, value string) {
	if display != "" {
		l.displayField = display
	}
	if value != "" {
		l.valueField = value
	}
	l.bones.forget()
	l.fromSource = nil
	l.Update()
}

// showing and meaning are the two field names, each falling back to the one a
// made source writes.
func (l *ListView) showing() string {
	if l.displayField != "" {
		return l.displayField
	}
	return rowDisplay
}

func (l *ListView) meaning() string {
	if l.valueField != "" {
		return l.valueField
	}
	return rowValue
}

// ValueAt is what the row at a position MEANS, as against what it shows.
//
// Nil for a blank row and for one whose record does not carry the field, which
// are different things the caller can tell apart by asking whether the row is
// there at all.
func (l *ListView) ValueAt(index int) *serval.Value {
	if index < 0 || index >= l.Count() {
		return nil
	}
	l.window(index, 1)
	id, ok := l.bones.idAt(index)
	if !ok {
		return nil
	}
	return l.values[serval.Key(id)]
}
