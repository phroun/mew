package display

// One app's store: a flat set of names, each holding one blob.
//
// This is NOT a filesystem. A filesystem is coming and this is not the early
// version of it: an app's store is nearer to cookies or a browser's local
// storage. There are no directories, nothing nests, and a key with a path in it
// would say otherwise.
//
// A key is a NAME: the app's own word for the item, and nothing else's. It is
// NOT the name a bundle carries -- a bundle states its own key and version
// inside itself, and the two versions of one bundle cannot share a store key
// because a store holds one thing per key. Nothing parses a store key, so the
// rules on it are the store's own and are not about addressing: see
// storeKeyRules.
//
// ONE namespace, and the key says how long the item lives: a key beginning with
// the cache mark names something the desktop may throw away at any moment,
// exactly as a `#` names a temporary table in SQL. Durability is part of the
// name rather than a place the item sits in, so it is visible everywhere the key
// is and there is no second namespace where the same key means something else.
//
// The two directories behind it are the desktop's business. What goes on disk is
// a cleaned form of the key with the item's type as the extension, and an index
// beside the files says which key each one belongs to. The key is what the app
// knows; the filename is what the filesystem will take; neither has to be the
// other.
//
// Bundles are what this is being built for, and it knows ONE thing about them:
// which item holds which. A psl carrying a `_bundle` member is noted on the way
// past so that a later question -- which item holds the bundle called X at
// version Y -- has an answer that does not mean reading the whole store. See
// bundleindex.go. Everything else about a bundle is somebody else's: this
// stores blobs.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The types an item may be, and the extension each one gets. An app naming
// anything else is refused: the extension is what the desktop and the user
// will read the file as later, so it cannot be whatever a connection says.
var storeTypes = map[string]bool{
	"txt":  true,
	"psl":  true,
	"bin":  true,
	"ini":  true,
	"conf": true,
}

// storeTypeList is the whitelist in the order a refusal names it.
func storeTypeList() []string {
	out := make([]string, 0, len(storeTypes))
	for t := range storeTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// The name of the index, which is not itself an item: it is never listed, and
// no key may be filed under it.
const storeIndexName = "index"

// storeChunk is how much of an item one store_data event carries. An app asking
// for something large gets it in pieces rather than one statement the size of
// the file, so nothing on either side has to hold the whole of it to make
// progress.
const storeChunk = 2048

// cacheMark begins the key of an item the desktop may throw away. It is the
// only difference between something kept and something cached -- there is one
// namespace, and this says which half of it a name is in.
const cacheMark = "#"

// cached reports whether a key names something discardable.
func cached(key string) bool { return strings.HasPrefix(key, cacheMark) }

// storeKeyRules says whether a key is one this store will take, and why not
// where it will not.
//
// Both rules are the STORE's, and neither is about addressing -- a store key
// never appears in an address, because an include names a bundle by key and
// version and the bundle index is what turns that into an item:
//
//   - No slash. A store is not a filesystem: it is a flat set of names, nearer
//     to cookies or a browser's local storage than to directories. A path in a
//     key would invite an app to treat it as the filesystem it is not, which is
//     why this is refused rather than merely discouraged.
//   - Nothing unprintable. The index beside the files is a line per item, so a
//     key with a newline in it writes a line that reads back as a different
//     item, silently.
//
// The cache mark leads the key or does not appear: one at the front says the
// item is discardable, and one anywhere else would be a mark that marks
// nothing. Every other rule is about the NAME, which is the key without it.
func storeKeyRules(key string) error {
	if key == "" {
		return fmt.Errorf("an item needs a key")
	}
	name := strings.TrimPrefix(key, cacheMark)
	if name == "" {
		return fmt.Errorf("%q is a cache mark and no name", key)
	}
	if strings.Contains(name, cacheMark) {
		return fmt.Errorf("the cache mark begins a key or is not in it: %q holds"+
			" one where it says nothing", key)
	}
	if strings.ContainsRune(name, '/') {
		return fmt.Errorf("a key is a name, not a path: %q holds a slash, which"+
			" separates the levels of an address", key)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("a key is written down as it stands: %q holds a"+
				" character that cannot be", key)
		}
	}
	return nil
}

// storeItem is one entry: what the app calls it, what it is, what it is filed
// under, how big it is, and what it hashes to.
//
// SIZE and HASH answer different questions at different moments. Size is the
// cursor of an upload in progress -- it says how much has landed, so an app
// knows where to carry on from -- and it means something at every moment of
// one. The hash means nothing until the upload has finished, and then it is the
// only thing that says whether what is here is already what the app holds.
type storeItem struct {
	key  string
	typ  string
	safe string
	size int64

	// hash is sha256 over the bytes, lowercase hex, and "" where it has not
	// been worked out. An app compares it against its own copy to decide
	// whether to upload at all, so being wrong here costs an upload silently
	// SKIPPED -- which is why it is settled inside the same lock as the write
	// that invalidates it, and never outside.
	hash string
}

// hashOf is what an item hashes to: sha256 over its bytes, lowercase hex.
//
// One definition, for a bundle and for anything else, and it is on the wire --
// an app works out its own copy's hash to compare, so a Go, a Python and a C
// client all have to arrive at the same string.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// storeDir is one app's folder in one tree.
type storeDir struct {
	dir string
	mu  sync.Mutex
}

func newStoreDir(dir string) *storeDir { return &storeDir{dir: dir} }

// appStore is one app's whole store: the names it keeps and the names it
// caches, in one flat namespace. The cache mark on a key is what decides which
// of the two directories the item's bytes sit in, so an app names things and
// the desktop files them.
type appStore struct {
	kept   *storeDir
	cached *storeDir
}

func newAppStore(host, app string) *appStore {
	return &appStore{
		kept:   newStoreDir(storagePath(dataTree, host, app)),
		cached: newStoreDir(storagePath(cacheTree, host, app)),
	}
}

// dirFor is where a key's bytes belong.
func (s *appStore) dirFor(key string) *storeDir {
	if cached(key) {
		return s.cached
	}
	return s.kept
}

// list is the whole inventory, both halves, by key. The cache mark sorts before
// every letter, so what is discardable reads first.
func (s *appStore) list() []storeItem {
	out := append(s.kept.list(), s.cached.list()...)
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (s *appStore) put(key, typ string, data []byte) (storeItem, error) {
	if err := storeKeyRules(strings.TrimSpace(key)); err != nil {
		return storeItem{}, err
	}
	return s.dirFor(key).put(key, typ, data)
}

func (s *appStore) appendTo(key, typ string, data []byte) (storeItem, error) {
	if err := storeKeyRules(strings.TrimSpace(key)); err != nil {
		return storeItem{}, err
	}
	return s.dirFor(key).appendTo(key, typ, data)
}

func (s *appStore) read(key string, offset int64) ([]byte, storeItem, bool, error) {
	return s.dirFor(key).read(key, offset)
}

func (s *appStore) drop(key string) error { return s.dirFor(key).drop(key) }

// list is the inventory: every item, by key.
//
// An item with no hash yet gets one here, and it is written down so the next
// inventory does not pay for it again. That is the whole of the laziness: an
// append leaves nothing to report, and the first question after it settles the
// matter for good.
func (s *storeDir) list() []storeItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.readLocked()

	var worked bool
	out := make([]storeItem, 0, len(items))
	for _, it := range items {
		it, filled := s.hashedLocked(items, it)
		worked = worked || filled
		out = append(out, s.sized(it))
	}
	if worked {
		_ = s.writeLocked(items)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// hashedLocked is an item with its hash filled in, and whether that cost a
// read. An item whose file will not open keeps its empty hash: saying nothing
// is right, where saying something would be a guess an app acts on.
func (s *storeDir) hashedLocked(items map[string]storeItem, it storeItem) (storeItem, bool) {
	if it.hash != "" {
		return it, false
	}
	data, err := os.ReadFile(s.pathOf(it))
	if err != nil {
		return it, false
	}
	it.hash = hashOf(data)
	items[it.key] = it
	return it, true
}

// put writes an item, replacing whatever the key held. A key whose type
// changes is refiled under the new extension and the old file goes: one key is
// one item, and leaving the old one behind would leave two files claiming it.
func (s *storeDir) put(key, typ string, data []byte) (storeItem, error) {
	return s.write(key, typ, data, false)
}

// appendTo adds to the end of an item, making it first if there is none. The
// type must be the one the item already has -- appending psl to a png is not
// something either of them survives.
func (s *storeDir) appendTo(key, typ string, data []byte) (storeItem, error) {
	return s.write(key, typ, data, true)
}

func (s *storeDir) write(key, typ string, data []byte, extend bool) (storeItem, error) {
	key = strings.TrimSpace(key)
	if err := storeKeyRules(key); err != nil {
		return storeItem{}, err
	}
	if !storeTypes[typ] {
		return storeItem{}, fmt.Errorf("type %q is not one this desktop stores; it stores %s",
			typ, strings.Join(storeTypeList(), ", "))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.readLocked()

	it, held := items[key]
	switch {
	case !held:
		it = storeItem{key: key, typ: typ, safe: s.freeNameLocked(items, key)}
	case extend && it.typ != typ:
		return storeItem{}, fmt.Errorf("%q is a %s item; it cannot be extended with %s",
			key, it.typ, typ)
	case it.typ != typ:
		// Replaced as a different type: the item keeps its name and loses the
		// file it was in, which no longer has the right extension.
		_ = os.Remove(s.pathOf(it))
		it.typ = typ
	}

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return storeItem{}, err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if extend {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(s.pathOf(it), flags, 0o600)
	if err != nil {
		return storeItem{}, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return storeItem{}, err
	}
	if err := f.Close(); err != nil {
		return storeItem{}, err
	}

	// A put holds the whole item, so its hash costs one pass over a buffer
	// beside the disk write just made. An append holds only the newest piece,
	// and the hash of a part-written item is not a fact about anything -- so it
	// is cleared, and worked out later by whoever asks, which is an app on a
	// connection long after the upload ended.
	if extend {
		it.hash = ""
	} else {
		it.hash = hashOf(data)
	}

	items[key] = it
	if err := s.writeLocked(items); err != nil {
		return storeItem{}, err
	}
	if err := s.noteBundleLocked(it); err != nil {
		return storeItem{}, err
	}
	return s.sized(it), nil
}

// drop removes an item: its bytes, and the index line that named it. The name
// it was filed under is free again afterwards, since nothing holds it and no
// file answers to it.
//
// Dropping what is not there succeeds. What the app asked for is that the key
// hold nothing, and it holds nothing -- so a clean-up that runs twice, or after
// a connection dropped mid-batch, is not an error to handle.
func (s *storeDir) drop(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("an item needs a key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.readLocked()
	it, held := items[key]
	if !held {
		return nil
	}
	if err := os.Remove(s.pathOf(it)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(items, key)
	if err := s.writeLocked(items); err != nil {
		return err
	}
	return s.forgetBundleLocked(key)
}

// read hands back one slice of an item, and says whether it is the last. An
// offset past the end reads as an empty last chunk rather than an error: it is
// what an app that read to the end and asked once more should be told.
func (s *storeDir) read(key string, offset int64) (data []byte, it storeItem, last bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	held, ok := s.readLocked()[strings.TrimSpace(key)]
	if !ok {
		return nil, storeItem{}, false, fmt.Errorf("no item is filed under %q", key)
	}
	it = s.sized(held)
	if offset < 0 {
		return nil, it, false, fmt.Errorf("an item is read from %d onwards, which is before it starts", offset)
	}
	if offset >= it.size {
		return nil, it, true, nil
	}

	f, err := os.Open(s.pathOf(it))
	if err != nil {
		return nil, it, false, err
	}
	defer f.Close()

	n := it.size - offset
	if n > storeChunk {
		n = storeChunk
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, it, false, err
	}
	return buf, it, offset+n >= it.size, nil
}

// pathOf is where an item's bytes live: its filed name, and its type as the
// extension.
func (s *storeDir) pathOf(it storeItem) string {
	return filepath.Join(s.dir, it.safe+"."+it.typ)
}

// sized fills in what the item currently weighs. A file that is not there
// weighs nothing, which is what an item written and then removed by hand
// should read as rather than an error nobody can act on.
func (s *storeDir) sized(it storeItem) storeItem {
	if info, err := os.Stat(s.pathOf(it)); err == nil {
		it.size = info.Size()
	}
	return it
}

// freeNameLocked is what a new key is filed under: the cleaned key, numbered if
// another item or another file already answers to it.
func (s *storeDir) freeNameLocked(items map[string]storeItem, key string) string {
	taken := map[string]bool{storeIndexName: true, bundleIndexName: true}
	for _, it := range items {
		taken[it.safe] = true
	}
	// Files already in the folder count too: an index lost or hand-edited
	// would otherwise hand a new key a name whose file is somebody else's.
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			taken[strings.TrimSuffix(name, filepath.Ext(name))] = true
		}
	}
	return safeName(key, "item", taken)
}

func (s *storeDir) indexPath() string { return filepath.Join(s.dir, storeIndexName) }

// readLocked reads the index: key -> what it is filed as.
func (s *storeDir) readLocked() map[string]storeItem {
	out := map[string]storeItem{}
	f, err := os.Open(s.indexPath())
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if it, ok := parseStoreLine(sc.Text()); ok {
			out[it.key] = it
		}
	}
	return out
}

func (s *storeDir) writeLocked(items map[string]storeItem) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# KittyTK stored items: <filed as>\\t<type>\\t<hash>\\t<key>\n")
	for _, k := range keys {
		it := items[k]
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\n", it.safe, it.typ, it.hash, it.key)
	}
	return os.WriteFile(s.indexPath(), []byte(sb.String()), 0o600)
}

// parseStoreLine reads one index line.
//
// The fields are TAB separated, which is what lets a key sit beside three
// others without being quoted: a key may hold spaces, and storeKeyRules refuses
// everything below 0x20, so a key can never hold a tab. The hash is empty where
// the item has not been hashed yet, which is two tabs together.
func parseStoreLine(line string) (storeItem, bool) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
		return storeItem{}, false
	}
	f := strings.Split(line, "\t")
	if len(f) != 4 {
		return storeItem{}, false
	}
	it := storeItem{safe: f[0], typ: f[1], hash: f[2], key: f[3]}
	if it.safe == "" || !storeTypes[it.typ] || it.key == "" {
		return storeItem{}, false
	}
	return it, true
}
