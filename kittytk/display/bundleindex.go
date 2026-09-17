package display

// Which stored item holds which bundle.
//
// A psl is a document like any other until it carries a `_bundle` member, and
// that member is the whole of what makes it a BUNDLE: something an include
// names by key and version rather than by where it happens to sit. So the store
// watches for one on the way past and writes down what it saw.
//
// It still knows nothing about what a bundle MEANS. It resolves no include and
// constructs nothing, and the hash it keeps is one it computed rather than one
// anybody vouched for. It answers one question -- which item
// holds the bundle called X at version Y, or the one hashing to H -- and the
// loader does everything after that.
//
// There is one of these per directory, beside that directory's own item index,
// so what is cached is indexed in the cache tree and what is kept in the data
// tree. A sweep of the cache takes its bundles' entries with it and cannot
// disturb the kept ones.
//
// The index is DERIVED and rebuildable. Nothing depends on it having been
// maintained: where the file is missing, the psl items are read and it is built
// again, which is what makes this safe to add to a store that already has
// bundles in it.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phroun/pawscript"
)

// bundleIndexName is the file. Like the item index it is not itself an item:
// nothing is filed under it and it is never listed.
const bundleIndexName = "bundles"

// bundleMark is the member that makes a document a bundle, and is also what a
// document must hold somewhere in its bytes to be worth parsing at all.
const bundleMark = "_bundle"

// bundleEntry is one bundle, and where it is.
//
// The version is "" where the bundle states none, which is a bundle like any
// other. There is no hash here: a bundle's hash is its ITEM's hash, and one
// hash per item computed in one place is better than two that have to agree.
// The store key is the join.
type bundleEntry struct {
	storeKey string // the item in this directory holding it
	key      string // _bundle.key, the name an include uses
	version  string // _bundle.version
}

// bundleOf reads a stored document and says which bundle it is, if any.
//
// A document that will not parse is not an error and not a refusal: a store
// takes arbitrary bytes and always has, so a psl that is not valid psl is
// simply not a bundle. That is also what makes this right for an item written
// in pieces -- a half-written bundle does not parse, so it is not indexed, and
// each further piece is tested again until the one that completes it. No
// "I have finished" signal has to be invented.
//
// A `_bundle` with no key is not indexed. The index exists to answer a question
// asked by name, and a bundle no include can name has no answer to give.
func bundleOf(storeKey string, data []byte) (bundleEntry, bool) {
	if !bytes.Contains(data, []byte(bundleMark)) {
		return bundleEntry{}, false
	}
	n, err := pawscript.ParsePSL(string(data))
	if err != nil || n == nil {
		return bundleEntry{}, false
	}
	meta, ok := n.Get(bundleMark)
	if !ok {
		return bundleEntry{}, false
	}
	block, ok := meta.(*pawscript.PSLNode)
	if !ok {
		return bundleEntry{}, false
	}
	e := bundleEntry{
		storeKey: storeKey,
		key:      pslText(block, "key"),
		version:  pslText(block, "version"),
	}
	if e.key == "" {
		return bundleEntry{}, false
	}
	return e, true
}

// pslText is a member read as text, whether it was written as a string or as a
// bare word: `key: figaro` and `key: "figaro"` name the same bundle, because
// the distinction PSL draws between a symbol and a string is not one a NAME
// makes.
//
// Anything else reads as nothing. A version in particular is text and is best
// written quoted -- a bare 1.0 is a number, and a number is not a version.
func pslText(n *pawscript.PSLNode, member string) string {
	v, ok := n.Get(member)
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case pawscript.Symbol:
		return string(x)
	}
	return ""
}

// writable reports whether an entry can be written down and read back whole.
// The index is tab separated and a line per bundle, so a field holding a tab or
// a newline would read back as a different entry, or as two.
func (e bundleEntry) writable() bool {
	for _, f := range []string{e.key, e.version, e.storeKey} {
		if strings.ContainsAny(f, "\t\n\r") {
			return false
		}
	}
	return true
}

func (s *storeDir) bundleIndexPath() string {
	return filepath.Join(s.dir, bundleIndexName)
}

// bundlesLocked is the index, by store key, built again where the file is gone.
func (s *storeDir) bundlesLocked() map[string]bundleEntry {
	f, err := os.Open(s.bundleIndexPath())
	if err != nil {
		return s.rebuildBundlesLocked()
	}
	defer f.Close()
	out := map[string]bundleEntry{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if e, ok := parseBundleLine(sc.Text()); ok {
			out[e.storeKey] = e
		}
	}
	return out
}

// rebuildBundlesLocked reads every psl item in this directory and says what it
// finds, without writing anything. Building it in memory is enough to answer
// with, and a store nobody has written a bundle to never grows a file it does
// not need.
func (s *storeDir) rebuildBundlesLocked() map[string]bundleEntry {
	out := map[string]bundleEntry{}
	for key, it := range s.readLocked() {
		if it.typ != "psl" {
			continue
		}
		data, err := os.ReadFile(s.pathOf(it))
		if err != nil {
			continue
		}
		if e, ok := bundleOf(key, data); ok && e.writable() {
			out[key] = e
		}
	}
	return out
}

func (s *storeDir) writeBundlesLocked(all map[string]bundleEntry) error {
	if len(all) == 0 {
		// Nothing to say, and a file saying nothing is a file to keep in step.
		if err := os.Remove(s.bundleIndexPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# KittyTK bundles: <key>\\t<version>\\t<stored under>\n")
	for _, k := range keys {
		e := all[k]
		fmt.Fprintf(&sb, "%s\t%s\t%s\n", e.key, e.version, e.storeKey)
	}
	return os.WriteFile(s.bundleIndexPath(), []byte(sb.String()), 0o600)
}

// parseBundleLine reads one line back. Every field is written as it stands, so
// all four are taken whole and none may hold the separator.
func parseBundleLine(line string) (bundleEntry, bool) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
		return bundleEntry{}, false
	}
	f := strings.Split(line, "\t")
	if len(f) != 3 {
		return bundleEntry{}, false
	}
	e := bundleEntry{key: f[0], version: f[1], storeKey: f[2]}
	if e.key == "" || e.storeKey == "" {
		return bundleEntry{}, false
	}
	return e, true
}

// noteBundleLocked writes down what an item just written is, or that it is no
// longer a bundle.
//
// The bytes are read back from disk rather than taken from the write, because
// an item written in pieces has only the newest piece to hand and it is the
// WHOLE document that is or is not a bundle. That means a bundle appended in n
// pieces is parsed n times, which is the price of not having to be told when
// the last one lands.
//
// An item that stops being a bundle loses its entry, which covers all three
// ways that happens: replaced with a psl that is not one, replaced with some
// other type entirely, or truncated back to something that will not parse.
func (s *storeDir) noteBundleLocked(it storeItem) error {
	all := s.bundlesLocked()
	_, had := all[it.key]

	var found bundleEntry
	var is bool
	if it.typ == "psl" {
		if data, err := os.ReadFile(s.pathOf(it)); err == nil {
			found, is = bundleOf(it.key, data)
			is = is && found.writable()
		}
	}

	switch {
	case is:
		all[it.key] = found
	case had:
		delete(all, it.key)
	default:
		return nil // it was not one and is not one: nothing to write
	}
	return s.writeBundlesLocked(all)
}

// forgetBundleLocked drops an item's entry, an item that is gone holding
// nothing.
func (s *storeDir) forgetBundleLocked(key string) error {
	all := s.bundlesLocked()
	if _, had := all[key]; !had {
		return nil
	}
	delete(all, key)
	return s.writeBundlesLocked(all)
}

// bundles is every bundle this directory holds, by store key.
func (s *storeDir) bundles() []bundleEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.bundlesLocked()
	out := make([]bundleEntry, 0, len(all))
	for _, e := range all {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].storeKey < out[j].storeKey })
	return out
}

// bundlesNamed is every item claiming this bundle key and version.
//
// **Cached shadows kept.** Where a cached item and a kept one both claim a key
// and version, the cached one is the answer and the kept one is not returned:
// the cache is where the fresher copy lands, and an item in it is the one an
// app most recently put there.
//
// This is an answer AT A MOMENT and not a binding. Nothing here is consulted
// again once a source has been built out of what it said, so a cached item
// evicted afterwards cannot disturb a source already running -- the shadowing
// happened at load, and it happens again, possibly differently, at the next
// load. That is what makes it safe for the cache to win: there is no live
// choice for an eviction to change underneath anybody.
//
// More than one answer is a COLLISION and is handed back whole rather than
// picked between. Two items claiming one bundle at one version is an authoring
// mistake, and a store that quietly chose one would hide it from the only party
// that could report it.
//
// An empty version matches an empty version. A bundle that states none is not a
// wildcard, and asking for one is asking for the bundle that states none.
func (s *appStore) bundlesNamed(key, version string) []bundleEntry {
	named := func(all []bundleEntry) []bundleEntry {
		var out []bundleEntry
		for _, e := range all {
			if e.key == key && e.version == version {
				out = append(out, e)
			}
		}
		return out
	}
	if hit := named(s.cached.bundles()); len(hit) > 0 {
		return hit
	}
	return named(s.kept.bundles())
}

// bundlesHashed is every bundle whose ITEM hashes to this, on the same terms.
//
// A hash names content rather than a name and a version, so two items answering
// to one hash are two copies of one bundle rather than a mistake -- but which
// item to read is still not this store's to decide, so both are handed back.
func (s *appStore) bundlesHashed(hash string) []bundleEntry {
	if hash == "" {
		return nil // an item not yet hashed is not found by not having one
	}
	if hit := s.cached.bundlesHashed(hash); len(hit) > 0 {
		return hit
	}
	return s.kept.bundlesHashed(hash)
}

// bundlesHashed joins the two indexes this directory keeps: the bundles, and
// the items they are. An item with no hash yet gets one, which is the same
// laziness the inventory has.
func (s *storeDir) bundlesHashed(hash string) []bundleEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.readLocked()

	var out []bundleEntry
	var worked bool
	for _, e := range s.bundlesLocked() {
		it, held := items[e.storeKey]
		if !held {
			continue
		}
		it, filled := s.hashedLocked(items, it)
		worked = worked || filled
		if it.hash == hash {
			out = append(out, e)
		}
	}
	if worked {
		_ = s.writeLocked(items)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].storeKey < out[j].storeKey })
	return out
}
