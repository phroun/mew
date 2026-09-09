package main

// The demo's store: what it asks the desktop it has kept for it, and the sample
// material it puts there.
//
// Subscribing pushes the inventory, so the first thing the status bar says is
// what last run left -- nothing, the first time. Then it writes the samples,
// each write answering with what the item now is. Nothing here is UI: it is the
// store seen from an app's end.
//
// A key beginning with `#` names something the desktop may throw away, so the
// samples the demo could rebuild carry one and the rest do not.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
)

// sample is one thing the demo keeps: the key it calls it by, what it is, and
// what is in it.
type sample struct {
	key  string
	typ  string
	body []byte
}

// samples is what the demo puts in its store. The two marked ones it could
// rebuild, so it lets the desktop throw them away: Clear Cache in the
// Connections window does exactly that, and leaves the rest standing.
func samples() []sample {
	return []sample{
		{"fruit-catalog", "txt", []byte(fruitList)},
		{"department-registry", "psl", []byte(departmentRegistry)},
		{"mime-types", "conf", []byte(mimeTypes)},
		{"settings", "ini", []byte(demoSettings)},
		{"seal", "bin", byteRamp(1)},
		{client.CacheMark + "orchard-thumbnail", "bin", byteRamp(24)},
		{client.CacheMark + "search-index", "txt", []byte(searchIndex)},
	}
}

// byteRamp is a blob holding every byte value there is, repeated -- something
// to look at in a hex dump, and something whose exact size is known: 256 bytes
// a lap.
func byteRamp(laps int) []byte {
	out := make([]byte, 0, laps*256)
	for lap := 0; lap < laps; lap++ {
		for b := 0; b < 256; b++ {
			out = append(out, byte(b))
		}
	}
	return out
}

// storeChunk is how much of a blob one statement carries. Real material is
// bigger than one statement wants to be, so the demo writes it the way
// anything large is written: the first piece replaces what was there, and the
// rest are appended.
const storeChunk = 2048

// stockTheStore subscribes -- which pushes the inventory -- and then writes the
// samples. It runs off the main path: the window is up and usable while it
// happens.
func (a *app) stockTheStore() {
	a.shelf.reset("before")
	a.watchStore()
	go func() {
		a.shelf.await()
		a.shelf.reset("after")
		a.writeSamples(samples())
		_ = a.conn.Store().List()
	}()
}

// writeSamples puts each sample in the store, continuing the ones larger than a
// statement wants to carry. The id to continue with comes back in the answer, so
// a write waits for it before appending to what it made.
func (a *app) writeSamples(samples []sample) {
	for _, s := range samples {
		body := s.body
		first := body
		if len(first) > storeChunk {
			first = first[:storeChunk]
		}
		if err := a.conn.Store().Write(s.key, s.typ, first); err != nil {
			return
		}
		if len(first) == len(body) {
			continue
		}
		id, ok := a.shelf.waitFor(s.key)
		if !ok {
			continue // the write was refused; its store_error said why
		}
		for at := len(first); at < len(body); at += storeChunk {
			end := at + storeChunk
			if end > len(body) {
				end = len(body)
			}
			if err := a.conn.Blob(id).Append(body[at:end]); err != nil {
				return
			}
		}
	}
}

// storeReport is what the demo knows about its store: the id each key is
// addressed by, and the inventory as it arrives, so the whole of a listing can
// be said in one line when the end of it comes.
type storeReport struct {
	mu    sync.Mutex
	when  string
	lines []string
	ids   map[string]uint64
	ended chan struct{}
}

func (r *storeReport) reset(when string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.when, r.lines = when, nil
	if r.ids == nil {
		r.ids = map[string]uint64{}
	}
	r.ended = make(chan struct{})
}

// note records what an answer said about one item.
func (r *storeReport) note(key, line string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if r.ids == nil {
		r.ids = map[string]uint64{}
	}
	r.ids[key] = id
}

// waitFor is the id a blob is addressed by, once the answer to writing it has
// arrived. A write SENDS: the id comes back as an answer, on the connection's own
// event goroutine, so continuing a blob means waiting to be told what to
// continue rather than asking straight away and finding nothing.
func (r *storeReport) waitFor(key string) (uint64, bool) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		id, ok := r.ids[key]
		r.mu.Unlock()
		if ok {
			return id, true
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// take hands back the lines gathered and clears them, so a second listing does
// not read as twice as full.
func (r *storeReport) take() (string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := r.lines
	r.lines = nil
	sort.Strings(lines)
	return r.when, lines
}

// end and await let the writing wait for the listing that subscribing pushed,
// so what the demo writes is not mixed into what it found.
func (r *storeReport) end() {
	r.mu.Lock()
	ended := r.ended
	r.mu.Unlock()
	if ended != nil {
		select {
		case <-ended:
		default:
			close(ended)
		}
	}
}

func (r *storeReport) await() {
	r.mu.Lock()
	ended := r.ended
	r.mu.Unlock()
	if ended == nil {
		return
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
	}
}

// watchStore subscribes to the store's answers and says what they were. An app
// that asks for nothing hears nothing, so this is what turns the store on -- and
// subscribing is also what asks for the inventory.
func (a *app) watchStore() {
	a.conn.OnStore(client.StoreBlob, func(ev *protocol.Event) {
		key, _ := ev.Text("key")
		typ, _ := ev.Word("type")
		size, _ := ev.Int("size")
		id, _ := ev.Uint("blob")
		a.shelf.note(key, fmt.Sprintf("%s.%s %s", key, typ, byteCount(size)), id)
	})
	a.conn.OnStore(client.StoreDone, func(ev *protocol.Event) {
		count, _ := ev.Int("count")
		when, lines := a.shelf.take()
		if count == 0 {
			a.setStatus(fmt.Sprintf("Store (%s): empty", when))
		} else {
			a.setStatus(fmt.Sprintf("Store (%s): %d items -- %s",
				when, count, strings.Join(lines, ", ")))
		}
		a.shelf.end()
	})
	a.conn.OnStore(client.StoreGone, func(ev *protocol.Event) {
		key, _ := ev.Text("key")
		a.setStatus("Store: " + key + " dropped")
	})
	a.conn.OnStore(client.StoreError, func(ev *protocol.Event) {
		reason, _ := ev.Text("reason")
		key, _ := ev.Text("key")
		if key == "" {
			a.setStatus("Store: " + reason)
			return
		}
		a.setStatus(fmt.Sprintf("Store: %s -- %s", key, reason))
	})
}

// byteCount is a size in the same shorthand the Connections window uses, so
// what the app says it wrote reads like what the desktop says it holds.
func byteCount(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%db", n)
}

// --- the samples themselves ---------------------------------------------

const fruitList = `apple
apricot
banana
blackcurrant
cherry
clementine
damson
elderberry
fig
gooseberry
greengage
kiwi
lemon
lychee
mango
medlar
mulberry
nectarine
papaya
passionfruit
peach
pear
persimmon
plum
pomegranate
quince
rambutan
raspberry
redcurrant
rhubarb
sloe
strawberry
tamarind
tangerine
`

// A PSL list with both collections in use: keyed members carrying the
// registry's own details, and ordered members carrying its records.
const departmentRegistry = `(
  _registry: (
    company: "Figaro Instruments",
    compiled: "2026-09-09",
    site: "Northgate"
  ),
  ("Engineering", head: "R. Alvarez", floor: 3, headcount: 41, budget: 2840000),
  ("Industrial Design", head: "M. Okonkwo", floor: 3, headcount: 12, budget: 610000),
  ("Acoustics", head: "P. Lindqvist", floor: 1, headcount: 9, budget: 445000),
  ("Field Service", head: "D. Achterberg", floor: 0, headcount: 27, budget: 1120000),
  ("Archive", head: "S. Mbeki", floor: -1, headcount: 3, budget: 88000),
  ("Reception", head: "T. Varga", floor: 0, headcount: 4, budget: 96000)
)
`

const mimeTypes = `# extension = media type
# What the demo would offer a file chooser, kept where a file chooser
# could read it back.

conf = text/plain
css  = text/css
csv  = text/csv
flac = audio/flac
gif  = image/gif
html = text/html
ini  = text/plain
jpg  = image/jpeg
js   = text/javascript
json = application/json
md   = text/markdown
mp4  = video/mp4
ogg  = audio/ogg
pdf  = application/pdf
png  = image/png
psl  = application/x-pawscript-list
svg  = image/svg+xml
tar  = application/x-tar
toml = application/toml
txt  = text/plain
wav  = audio/wav
webp = image/webp
xml  = application/xml
zip  = application/zip
`

const demoSettings = `[window]
tab = trinkets
direction = ltr

[details]
show_key = true
fit_width = false
pinned_left = 0
pinned_right = 0

[terminal]
shell = default
`

const searchIndex = `# A scratch index: rebuildable, so it lives in the cache.
apple      fruit-catalog:1
acoustics  department-registry:3
archive    department-registry:5
banana     fruit-catalog:3
engineering department-registry:1
png        mime-types:15
psl        mime-types:16
`
