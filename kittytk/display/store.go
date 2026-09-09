package display

// One app's shelf: a key-value store of blobs, kept in the app's own folder
// under whichever of the two trees it named.
//
// This is NOT a filesystem. A filesystem is coming and this is not the early
// version of it: an app's shelf is nearer to cookies or a browser's local
// storage -- a flat set of names, each holding one thing. There are no
// directories, nothing nests, and a key with a path in it would say otherwise.
//
// A key is a NAME. It is the app's own word for the item, and it is the same
// name the item carries when it becomes a bundle, so the two are one namespace
// rather than two that have to be kept in step. That is what the rules on it
// come from: see storeKeyRules.
//
// What goes on disk is a cleaned form of the key with the item's type as the
// extension, and an index beside the files says which key each one belongs to.
// The key is what the app knows; the filename is what the filesystem will take;
// neither has to be the other.
//
// Bundles are what this is being built for, and it knows nothing about them.
// It stores blobs.

import (
	"bufio"
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

// storeKeyRules says whether a key is one this store will take, and why not
// where it will not.
//
// A key is the same name the item carries as a bundle, and a bundle is
// addressed `<source>/<bundle>/<record>` -- so the three things an address
// needs to stay unambiguous are the three things a key may not be:
//
//   - No slash. It is the separator between the levels of an address, and a
//     key holding one could not be told from two levels. It is also the thing
//     that would make a shelf look like a filesystem, and a shelf is not one:
//     it is a flat set of names, nearer to cookies than to directories.
//   - No key of nothing but digits. All-digits is how an address says it means
//     the record at that INDEX, so `objectLibrary/7` could name a bundle or a
//     record and there is no way to say which.
//   - Nothing unprintable. The index beside the files is a line per item, so a
//     key with a newline in it writes a line that reads back as a different
//     item, silently.
func storeKeyRules(key string) error {
	if key == "" {
		return fmt.Errorf("an item needs a key")
	}
	if strings.ContainsRune(key, '/') {
		return fmt.Errorf("a key is a name, not a path: %q holds a slash, which"+
			" separates the levels of an address", key)
	}
	digits := true
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("a key is written down as it stands: %q holds a"+
				" character that cannot be", key)
		}
		if r < '0' || r > '9' {
			digits = false
		}
	}
	if digits {
		return fmt.Errorf("a key of nothing but digits is how an address names"+
			" a record by its position, so %q could not be told from one", key)
	}
	return nil
}

// storeItem is one entry: what the app calls it, what it is, what it is filed
// under, and how big it is.
type storeItem struct {
	key  string
	typ  string
	safe string
	size int64
}

// appStore is one app's folder in one tree.
type appStore struct {
	dir string
	mu  sync.Mutex
}

func newAppStore(dir string) *appStore { return &appStore{dir: dir} }

// list is the inventory: every item, by key.
func (s *appStore) list() []storeItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.readLocked()
	out := make([]storeItem, 0, len(items))
	for _, it := range items {
		out = append(out, s.sized(it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// put writes an item, replacing whatever the key held. A key whose type
// changes is refiled under the new extension and the old file goes: one key is
// one item, and leaving the old one behind would leave two files claiming it.
func (s *appStore) put(key, typ string, data []byte) (storeItem, error) {
	return s.write(key, typ, data, false)
}

// appendTo adds to the end of an item, making it first if there is none. The
// type must be the one the item already has -- appending psl to a png is not
// something either of them survives.
func (s *appStore) appendTo(key, typ string, data []byte) (storeItem, error) {
	return s.write(key, typ, data, true)
}

func (s *appStore) write(key, typ string, data []byte, extend bool) (storeItem, error) {
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

	items[key] = it
	if err := s.writeLocked(items); err != nil {
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
func (s *appStore) drop(key string) error {
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
	return s.writeLocked(items)
}

// read hands back one slice of an item, and says whether it is the last. An
// offset past the end reads as an empty last chunk rather than an error: it is
// what an app that read to the end and asked once more should be told.
func (s *appStore) read(key string, offset int64) (data []byte, it storeItem, last bool, err error) {
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
func (s *appStore) pathOf(it storeItem) string {
	return filepath.Join(s.dir, it.safe+"."+it.typ)
}

// sized fills in what the item currently weighs. A file that is not there
// weighs nothing, which is what an item written and then removed by hand
// should read as rather than an error nobody can act on.
func (s *appStore) sized(it storeItem) storeItem {
	if info, err := os.Stat(s.pathOf(it)); err == nil {
		it.size = info.Size()
	}
	return it
}

// freeNameLocked is what a new key is filed under: the cleaned key, numbered if
// another item or another file already answers to it.
func (s *appStore) freeNameLocked(items map[string]storeItem, key string) string {
	taken := map[string]bool{storeIndexName: true}
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

func (s *appStore) indexPath() string { return filepath.Join(s.dir, storeIndexName) }

// readLocked reads the index: key -> what it is filed as.
func (s *appStore) readLocked() map[string]storeItem {
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

func (s *appStore) writeLocked(items map[string]storeItem) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# KittyTK stored items: <filed as> <type> <key>\n")
	for _, k := range keys {
		it := items[k]
		fmt.Fprintf(&sb, "%s %s %s\n", it.safe, it.typ, it.key)
	}
	return os.WriteFile(s.indexPath(), []byte(sb.String()), 0o600)
}

// parseStoreLine reads one index line. The key is the remainder of the line,
// so it may hold spaces; the two fields before it cannot.
func parseStoreLine(line string) (storeItem, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return storeItem{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return storeItem{}, false
	}
	at := 0
	for _, f := range fields[:2] {
		at += strings.Index(line[at:], f) + len(f)
	}
	it := storeItem{safe: fields[0], typ: fields[1], key: strings.TrimSpace(line[at:])}
	if it.safe == "" || !storeTypes[it.typ] || it.key == "" {
		return storeItem{}, false
	}
	return it, true
}
