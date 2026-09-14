package source

// Where a record stood, remembered by whoever said so.
//
// A scope names its ends by identity, and an identity is not a position: it
// says which record, not where. A source that holds the records resolves one
// by looking it up. A source built on top of others cannot -- the records are
// somewhere else, and asking their owner "where is this?" is not a question the
// interface has -- so it remembers instead, as it hands each record on.
//
// The notes belong to the prepared sequence -- this source, this sort, this
// filter -- and not to any one reading of it. Those three are what decide which
// records are in a sequence and where they stand, so two readers naming the
// same three are reading the same sequence and a record stands in the same
// place for both. It is the same thing `dataSetKey` already dedupes an ordering
// by, and it is kept beside them for the same reason.
//
// That is not a cache of the data. It is a note of where this source put a
// record when it last said something about it, which is exactly what it needs
// to answer "and what comes after that one?" -- and it is bounded, because a
// reader that walks a million records should not leave a million notes behind.

import (
	"sync"

	"github.com/phroun/kittytk/wire"
)

// placesKept is how many positions one source remembers. A reader asks from
// where it got to, so the notes that matter are the recent ones; far more than
// a screenful is already generosity, and this is generous.
const placesKept = 1024

// A places remembers where records stood, oldest forgotten first.
type places struct {
	at   map[string][]*wire.Value
	seen []string
}

func newPlaces() *places {
	return &places{at: map[string][]*wire.Value{}}
}

// put notes where a record stood. Noting the same record twice moves it to the
// front of the queue rather than storing it again, because a record the reader
// keeps passing is one it is likely to ask from.
func (p *places) put(id *wire.Value, tuple []*wire.Value) {
	if id == nil {
		return
	}
	k := wire.EncodeValue(id)
	if _, had := p.at[k]; !had {
		p.seen = append(p.seen, k)
	}
	p.at[k] = tuple
	for len(p.seen) > placesKept {
		old := p.seen[0]
		p.seen = p.seen[1:]
		delete(p.at, old)
	}
}

// get is where a record stood, and false for one this source has not placed --
// which it has not if it never sent it, or sent it so long ago that the note
// has been forgotten.
func (p *places) get(id *wire.Value) ([]*wire.Value, bool) {
	if id == nil {
		return nil, false
	}
	t, ok := p.at[wire.EncodeValue(id)]
	return t, ok
}

// A placebook is one source's notes, kept per prepared sequence.
//
// A query is asked once and answered once, and a reader may let one data set
// go and open another over the same sequence between two scopes of it. What
// resumes a scope is therefore not the reading -- it is the sequence, which the
// source and the sort and the filter name between them, and which is what this
// is keyed by.
//
// Bounded the way the orderings are, and for the same reason: each entry is a
// sequence somebody is scrolling, the recent ones are the ones being scrolled,
// and an unbounded pile of them is not worth keeping.
type placebook struct {
	mu     sync.Mutex
	books  map[string][]*places
	recent []string
}

func newPlacebook() *placebook {
	return &placebook{books: map[string][]*places{}}
}

// of is the notes for one sequence, made if this is the first scope of it.
// Each sequence gets `n` of them, because a source may have more than one thing
// to remember about the same record.
func (b *placebook) of(spec *wire.Spec, n int) []*places {
	key := dataSetKey(spec)
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.books[key]; p != nil {
		return p
	}
	p := make([]*places, n)
	for i := range p {
		p[i] = newPlaces()
	}
	b.books[key] = p
	b.recent = append(b.recent, key)
	for len(b.recent) > orderingsKept {
		delete(b.books, b.recent[0])
		b.recent = b.recent[1:]
	}
	return p
}
