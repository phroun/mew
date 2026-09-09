package display

// An app's shelf: what a key is filed as, what survives being written and read
// back, and what the store refuses.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// shelf is a store in a directory of this test's own.
func shelf(t *testing.T) *appStore {
	t.Helper()
	return newAppStore(filepath.Join(t.TempDir(), "app"))
}

// everyByte is a payload holding all 256 values, which is what tells a
// byte-exact round trip from one that went through text.
func everyByte() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// whole reads an item back the way an app does: a chunk at a time from where
// the last one ended, until the one marked last.
func whole(t *testing.T, s *appStore, key string) []byte {
	t.Helper()
	var out []byte
	for offset := int64(0); ; {
		data, _, last, err := s.read(key, offset)
		if err != nil {
			t.Fatalf("reading %q at %d: %v", key, offset, err)
		}
		out = append(out, data...)
		offset += int64(len(data))
		if last {
			return out
		}
		if len(data) == 0 {
			t.Fatalf("reading %q at %d returned nothing and was not the last chunk",
				key, offset)
		}
	}
}

// What goes in comes out, byte for byte. An item is a blob: the store is not
// entitled to an opinion about what is in one.
func TestWhatIsStoredComesBackByteForByte(t *testing.T) {
	s := shelf(t)
	want := everyByte()
	if _, err := s.put("figaro", "bin", want); err != nil {
		t.Fatal(err)
	}
	if got := whole(t, s, "figaro"); !bytes.Equal(got, want) {
		t.Errorf("read back %d bytes of %d", len(got), len(want))
	}
}

// The key is the app's own word for the item and the filename is what the
// filesystem will take. A key with punctuation in it is not a file called
// that, and what the app asks for it by does not change.
func TestAKeyIsFiledUnderACleanedFormOfItself(t *testing.T) {
	s := shelf(t)
	it, err := s.put("Figaro's Bundle!", "psl", []byte("(a: 1)"))
	if err != nil {
		t.Fatal(err)
	}
	if it.safe != "figaro-s-bundle" {
		t.Errorf("filed under %q", it.safe)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "figaro-s-bundle.psl")); err != nil {
		t.Errorf("the file is not where the index says: %v", err)
	}
	// The key it answers to is still the app's own.
	items := s.list()
	if len(items) != 1 || items[0].key != "Figaro's Bundle!" {
		t.Errorf("the inventory reads as %+v", items)
	}
}

// Two keys that clean to the same word are two items in two files. Sharing one
// would have the second silently overwrite the first.
func TestTwoKeysThatCleanAlikeAreTwoItems(t *testing.T) {
	s := shelf(t)
	for i, key := range []string{"my notes", "My  Notes!"} {
		if _, err := s.put(key, "txt", []byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(whole(t, s, "my notes")); got != "a" {
		t.Errorf("the first item reads as %q", got)
	}
	if got := string(whole(t, s, "My  Notes!")); got != "b" {
		t.Errorf("the second item reads as %q", got)
	}
	if n := len(s.list()); n != 2 {
		t.Errorf("the shelf holds %d items, want 2", n)
	}
}

// Appending extends what is there, and makes the item where there is none --
// which is how something larger than one statement is written.
func TestAppendingBuildsAnItemUpInPieces(t *testing.T) {
	s := shelf(t)
	for _, part := range []string{"one ", "two ", "three"} {
		if _, err := s.appendTo("log", "txt", []byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(whole(t, s, "log")); got != "one two three" {
		t.Errorf("the item reads as %q", got)
	}
	if n := len(s.list()); n != 1 {
		t.Errorf("three appends made %d items", n)
	}
}

// A put replaces. An item written again is what was written the second time,
// not the second thing added to the first.
func TestPuttingReplacesWhatWasThere(t *testing.T) {
	s := shelf(t)
	if _, err := s.put("notes", "txt", []byte("the first thing")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.put("notes", "txt", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if got := string(whole(t, s, "notes")); got != "second" {
		t.Errorf("the item reads as %q", got)
	}
}

// An item put again as another type is refiled, and the file under the old
// extension goes. Left behind, two files would answer to one key and the
// desktop would read whichever it found first.
func TestReplacingAnItemAsAnotherTypeLeavesNoOldFile(t *testing.T) {
	s := shelf(t)
	first, err := s.put("thing", "txt", []byte("words"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.put("thing", "psl", []byte("(a: 1)"))
	if err != nil {
		t.Fatal(err)
	}
	if second.safe != first.safe {
		t.Errorf("the item was refiled from %q to %q; it is the same item", first.safe, second.safe)
	}
	if _, err := os.Stat(filepath.Join(s.dir, first.safe+".txt")); err == nil {
		t.Error("the item's old file is still there under the old extension")
	}
	if got := string(whole(t, s, "thing")); got != "(a: 1)" {
		t.Errorf("the item reads as %q", got)
	}
}

// Appending to an item as another type is refused. There is no answer to
// what a text file with a PNG on the end of it is.
func TestAppendingAsAnotherTypeIsRefused(t *testing.T) {
	s := shelf(t)
	if _, err := s.put("thing", "txt", []byte("words")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.appendTo("thing", "bin", []byte{0}); err == nil {
		t.Error("a txt item was extended with bin")
	}
	if got := string(whole(t, s, "thing")); got != "words" {
		t.Errorf("the refused append changed the item to %q", got)
	}
}

// The type whitelist is the extension the file gets, so it is not whatever a
// connection says it is.
func TestOnlyTheKnownTypesAreStored(t *testing.T) {
	s := shelf(t)
	for _, typ := range []string{"txt", "psl", "bin", "ini", "conf"} {
		if _, err := s.put("k-"+typ, typ, []byte("x")); err != nil {
			t.Errorf("%s is a type this stores, and was refused: %v", typ, err)
		}
	}
	for _, typ := range []string{"sh", "exe", "html", "", "TXT", "psl.sh"} {
		if _, err := s.put("bad", typ, []byte("x")); err == nil {
			t.Errorf("an item was stored as %q", typ)
		}
	}
}

// An item needs a key. Filed under nothing it could not be asked for again.
func TestAnItemWithNoKeyIsRefused(t *testing.T) {
	s := shelf(t)
	for _, key := range []string{"", "   "} {
		if _, err := s.put(key, "txt", []byte("x")); err == nil {
			t.Errorf("an item was stored under %q", key)
		}
	}
}

// A key is a name, and the shelf is a flat set of them -- nearer to cookies
// than to directories. A slash is what separates the levels of an ADDRESS, so
// one inside a key could not be told from two levels; and it is what would
// have an app treat this as the filesystem, which it is not and which is
// coming separately.
func TestAKeyIsANameAndNotAPath(t *testing.T) {
	s := shelf(t)
	for _, key := range []string{
		"objects/figaro",
		"/leading",
		"trailing/",
		"a/b/c",
	} {
		if _, err := s.put(key, "txt", []byte("x")); err == nil {
			t.Errorf("%q was stored, so the shelf reads as a filesystem", key)
		}
	}
	// The same name without the path in it is fine, and so is punctuation that
	// separates nothing.
	for _, key := range []string{"objects-figaro", "figaro.v2", "my notes", "a_b"} {
		if _, err := s.put(key, "txt", []byte("x")); err != nil {
			t.Errorf("%q was refused: %v", key, err)
		}
	}
}

// A key of nothing but digits is how an address names a record by its
// position, so one would be a bundle that could not be told from an index.
func TestAKeyOfOnlyDigitsIsRefused(t *testing.T) {
	s := shelf(t)
	for _, key := range []string{"7", "01", "1234567"} {
		if _, err := s.put(key, "txt", []byte("x")); err == nil {
			t.Errorf("%q was stored, and could not be told from a record index", key)
		}
	}
	// Digits are only a problem when they are the whole of it.
	for _, key := range []string{"v7", "7a", "7-of-9", "1.0"} {
		if _, err := s.put(key, "txt", []byte("x")); err != nil {
			t.Errorf("%q was refused: %v", key, err)
		}
	}
}

// The index is a line per item, so a key with a line break in it would write a
// second line that reads back as a different item -- and the item it named
// would answer to a key nobody asked for.
func TestAKeyThatWouldBreakTheIndexIsRefused(t *testing.T) {
	s := shelf(t)
	for _, key := range []string{"two\nlines", "tab\there", "bell\x07", "null\x00"} {
		if _, err := s.put(key, "txt", []byte("x")); err == nil {
			t.Errorf("%q was stored, and the index no longer says what is where", key)
		}
	}
	if n := len(s.list()); n != 0 {
		t.Errorf("the shelf holds %d items after storing nothing storable", n)
	}
}

// Dropping an item takes its bytes and the line that named it, and leaves the
// rest of the shelf alone. An app that can only ever add fills a shelf it has
// no way to empty.
func TestDroppingAnItemTakesItAndNothingElse(t *testing.T) {
	s := shelf(t)
	for _, key := range []string{"keep", "drop", "also-keep"} {
		if _, err := s.put(key, "txt", []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	gone := s.list()[1] // the inventory is by key, so "drop" is the middle one
	if gone.key != "drop" {
		t.Fatalf("the inventory reads as %+v", s.list())
	}

	if err := s.drop("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, gone.safe+".txt")); err == nil {
		t.Error("the item's file outlived the item")
	}
	var keys []string
	for _, it := range s.list() {
		keys = append(keys, it.key)
	}
	if len(keys) != 2 || keys[0] != "also-keep" || keys[1] != "keep" {
		t.Errorf("the shelf reads as %v after one item was dropped", keys)
	}
	if _, _, _, err := s.read("drop", 0); err == nil {
		t.Error("the dropped item still reads back")
	}
	if got := string(whole(t, s, "keep")); got != "keep" {
		t.Errorf("a neighbour reads as %q", got)
	}
}

// Dropping what is not there succeeds: what was asked for is that the key hold
// nothing, and it does. A clean-up that runs twice is not an error to handle.
func TestDroppingWhatIsNotThereIsNotAnError(t *testing.T) {
	s := shelf(t)
	if _, err := s.put("thing", "txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.drop("thing"); err != nil {
			t.Errorf("drop %d: %v", i, err)
		}
	}
	if err := s.drop("never stored"); err != nil {
		t.Errorf("dropping a key nothing was under: %v", err)
	}
	// And a key needs saying, dropped or not.
	if err := s.drop("  "); err == nil {
		t.Error("a drop with no key was accepted")
	}
}

// The name a dropped item was filed under is free again: nothing holds it and
// no file answers to it, so the next key that cleans to it takes it bare rather
// than being numbered around a gap.
func TestADroppedItemsNameIsFreeAgain(t *testing.T) {
	s := shelf(t)
	first, err := s.put("notes", "txt", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.drop("notes"); err != nil {
		t.Fatal(err)
	}
	second, err := s.put("Notes!", "txt", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if second.safe != first.safe {
		t.Errorf("the freed name %q was not used again; the new item is %q",
			first.safe, second.safe)
	}
}

// An item larger than one chunk comes back in pieces, each saying where it
// starts, with the end marked. Nothing on either side holds the whole of it.
func TestALargeItemComesBackInChunks(t *testing.T) {
	s := shelf(t)
	want := bytes.Repeat([]byte("0123456789"), storeChunk/10*3+7)
	if _, err := s.put("big", "bin", want); err != nil {
		t.Fatal(err)
	}

	var got []byte
	chunks := 0
	for offset := int64(0); ; chunks++ {
		data, it, last, err := s.read("big", offset)
		if err != nil {
			t.Fatal(err)
		}
		if it.size != int64(len(want)) {
			t.Errorf("chunk %d says the item is %d bytes; it is %d", chunks, it.size, len(want))
		}
		if len(data) > storeChunk {
			t.Errorf("chunk %d is %d bytes, over the %d a chunk is", chunks, len(data), storeChunk)
		}
		got = append(got, data...)
		offset += int64(len(data))
		if last {
			break
		}
	}
	if chunks < 3 {
		t.Errorf("an item of %d bytes came back in %d chunks", len(want), chunks+1)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the pieces came back to %d bytes of %d", len(got), len(want))
	}
}

// Reading from the end is an empty last chunk, not an error: it is what an app
// that read to the end and asked once more should be told.
func TestReadingFromTheEndEndsIt(t *testing.T) {
	s := shelf(t)
	if _, err := s.put("thing", "txt", []byte("five!")); err != nil {
		t.Fatal(err)
	}
	data, _, last, err := s.read("thing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 || !last {
		t.Errorf("reading from the end gave %d bytes, last=%v", len(data), last)
	}
}

// An empty item is one empty chunk marked last, rather than a read that never
// ends because nothing ever arrives.
func TestAnEmptyItemIsOneEmptyChunk(t *testing.T) {
	s := shelf(t)
	if _, err := s.put("nothing", "txt", nil); err != nil {
		t.Fatal(err)
	}
	data, _, last, err := s.read("nothing", 0)
	if err != nil || len(data) != 0 || !last {
		t.Errorf("an empty item read as %d bytes, last=%v, err=%v", len(data), last, err)
	}
}

// A key nothing is filed under is an error rather than an empty item: an app
// that misremembers a key should be told, not handed nothing.
func TestReadingAKeyThatIsNotThereSaysSo(t *testing.T) {
	if _, _, _, err := shelf(t).read("never stored", 0); err == nil {
		t.Error("a key nothing is filed under read back without complaint")
	}
}

// The index is not an item. It is listed nowhere, and no key is filed under
// its name -- a key that cleaned to `index` would otherwise overwrite the
// record of where everything is.
func TestTheIndexIsNotAnItem(t *testing.T) {
	s := shelf(t)
	it, err := s.put("Index", "txt", []byte("mine"))
	if err != nil {
		t.Fatal(err)
	}
	if it.safe == storeIndexName {
		t.Fatalf("an item was filed as %q, over the index", it.safe)
	}
	items := s.list()
	if len(items) != 1 || items[0].key != "Index" {
		t.Errorf("the inventory reads as %+v", items)
	}
	if got := string(whole(t, s, "Index")); got != "mine" {
		t.Errorf("the item reads as %q", got)
	}
}

// The inventory survives the store being opened again: it is on disk, not in
// the object that wrote it.
func TestTheInventoryOutlivesTheStoreThatWroteIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	first := newAppStore(dir)
	if _, err := first.put("a key with spaces", "conf", []byte("x=1")); err != nil {
		t.Fatal(err)
	}

	items := newAppStore(dir).list()
	if len(items) != 1 {
		t.Fatalf("the reopened shelf holds %+v", items)
	}
	if items[0].key != "a key with spaces" || items[0].typ != "conf" || items[0].size != 3 {
		t.Errorf("the item reads back as %+v", items[0])
	}
}

// Which tree a statement names, and whether the connection has anywhere to put
// anything, are settled before the store is reached at all.
func TestAConnectionIsToldWhenThereIsNowhereToPutIt(t *testing.T) {
	tempConfig(t)
	known := admittedOnce(t, "sha256:aaa", "Demo", "Laptop")
	c := &conn{
		server:   &Server{known: known},
		identity: "sha256:aaa",
		appName:  "Demo",
	}

	for _, tree := range []string{dataTree, cacheTree} {
		if _, err := c.storeIn(tree); err != nil {
			t.Errorf("%s is a tree this desktop keeps, and was refused: %v", tree, err)
		}
	}
	for _, tree := range []string{"", "temp", "DATA", "../data"} {
		if _, err := c.storeIn(tree); err == nil {
			t.Errorf("a statement reached the shelf %q", tree)
		}
	}

	// A peer with no identity has no folder, and neither has an app this
	// desktop has not admitted. Writing anyway would put every such
	// connection's material in one place named after nobody.
	for _, at := range []struct{ identity, app string }{
		{"", "Demo"},
		{"sha256:aaa", "Never Admitted"},
		{"sha256:unknown", "Demo"},
	} {
		nobody := &conn{
			server:   c.server,
			identity: at.identity,
			appName:  at.app,
		}
		if _, err := nobody.storeIn(dataTree); err == nil {
			t.Errorf("identity %q app %q was given a shelf", at.identity, at.app)
		}
	}
}

// A file in the folder that no index mentions still holds its name. An index
// lost or hand-edited would otherwise file a new key under a name whose file
// belongs to something else.
func TestAFileNoIndexMentionsKeepsItsName(t *testing.T) {
	s := shelf(t)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "notes.txt"), []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}

	it, err := s.put("notes", "txt", []byte("newer"))
	if err != nil {
		t.Fatal(err)
	}
	if it.safe == "notes" {
		t.Fatal("the new item was filed over a file nothing accounted for")
	}
	if got, err := os.ReadFile(filepath.Join(s.dir, "notes.txt")); err != nil || string(got) != "older" {
		t.Errorf("the unaccounted file reads as %q (%v)", got, err)
	}
}
