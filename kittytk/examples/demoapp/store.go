package main

// The demo's shelf: what it asks the desktop it has kept for it, and the
// sample material it puts there.
//
// On connecting it reads its inventory -- which is empty the first time and
// holds last run's material every time after -- then writes the samples and
// reads the inventory again, so the status bar shows the shelf before and
// after. Nothing here is UI: it is the store seen from an app's end.

import (
	"fmt"
	"sort"
	"strings"
	"sync"

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

// dataSamples is what the demo keeps: material it would be sorry to lose.
func dataSamples() []sample {
	return []sample{
		{"catalog/fruit", "txt", []byte(fruitList)},
		{"registry/departments", "psl", []byte(departmentRegistry)},
		{"mime/types", "conf", []byte(mimeTypes)},
		{"settings", "ini", []byte(demoSettings)},
		{"seal", "bin", byteRamp(1)},
	}
}

// cacheSamples is what it can afford to lose: the desktop may throw any of it
// away, and Clear Cache in the Connections window does exactly that.
func cacheSamples() []sample {
	return []sample{
		{"thumbnails/orchard", "bin", byteRamp(24)},
		{"search/index", "txt", []byte(searchIndex)},
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

// stockTheShelf reads the inventory, writes the samples, and reads it again.
// It runs off the main path: the window is up and usable while it happens.
func (a *app) stockTheShelf() {
	a.watchStore()
	go func() {
		a.readInventory("before")
		a.writeSamples(a.conn.Data(), dataSamples())
		a.writeSamples(a.conn.Cache(), cacheSamples())
		a.readInventory("after")
	}()
}

// readInventory asks both trees what they hold. The answers arrive as events,
// which watchStore narrates.
func (a *app) readInventory(when string) {
	a.storeNarration(when)
	_ = a.conn.Data().List()
	_ = a.conn.Cache().List()
}

// writeSamples puts each sample on the shelf, in pieces where it is larger
// than one statement wants to carry.
func (a *app) writeSamples(store client.Store, samples []sample) {
	for _, s := range samples {
		body := s.body
		first := body
		if len(first) > storeChunk {
			first = first[:storeChunk]
		}
		if err := store.Put(s.key, s.typ, first); err != nil {
			return
		}
		for at := len(first); at < len(body); at += storeChunk {
			end := at + storeChunk
			if end > len(body) {
				end = len(body)
			}
			if err := store.Append(s.key, s.typ, body[at:end]); err != nil {
				return
			}
		}
	}
}

// storeReport gathers one tree's inventory as its items arrive, so the whole
// of it can be said in one line when the end of it comes.
type storeReport struct {
	mu    sync.Mutex
	when  string
	items map[string][]string
}

func (r *storeReport) reset(when string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.when = when
	r.items = map[string][]string{}
}

func (r *storeReport) add(tree, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = map[string][]string{}
	}
	r.items[tree] = append(r.items[tree], line)
}

// take hands back one tree's lines and clears them, so the next listing of
// that tree starts empty rather than reading as twice as full.
func (r *storeReport) take(tree string) (string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := r.items[tree]
	delete(r.items, tree)
	sort.Strings(lines)
	return r.when, lines
}

// storeNarration starts a fresh report for the listing about to happen.
func (a *app) storeNarration(when string) { a.shelf.reset(when) }

// watchStore subscribes to the store's answers and says what they were. An app
// that asks for nothing hears nothing, so this is what turns the shelf on.
func (a *app) watchStore() {
	a.conn.OnStore(client.StoreItem, func(ev *protocol.Event) {
		tree, _ := ev.Word("tree")
		key, _ := ev.Text("key")
		typ, _ := ev.Word("type")
		size, _ := ev.Int("size")
		a.shelf.add(tree, fmt.Sprintf("%s.%s %s", key, typ, byteCount(size)))
	})
	a.conn.OnStore(client.StoreDone, func(ev *protocol.Event) {
		tree, _ := ev.Word("tree")
		count, _ := ev.Int("count")
		when, lines := a.shelf.take(tree)
		if count == 0 {
			a.setStatus(fmt.Sprintf("Store (%s): %s is empty", when, tree))
			return
		}
		a.setStatus(fmt.Sprintf("Store (%s): %s holds %d -- %s",
			when, tree, count, strings.Join(lines, ", ")))
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
apple      catalog/fruit:1
acoustics  registry/departments:3
archive    registry/departments:5
banana     catalog/fruit:3
engineering registry/departments:1
png        mime/types:15
psl        mime/types:16
`
