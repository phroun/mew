package display

// Where a client's material is kept.
//
// Two parallel trees under the config directory, named the same way all the way
// down:
//
//	data/<host>/<app>/    kept until the user says otherwise
//	cache/<host>/<app>/   safe to delete at any moment
//
// The split is the whole point of having two: anything under cache/ can be
// thrown away without asking, and anything under data/ cannot. A host's own
// material sits directly in its folder, beside the app folders.
//
// This is not the box:// filesystem an application sees. It is the desktop's
// own shelf: nothing here is addressable over the wire, and an application is
// not told where -- or whether -- its material physically lives.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The two trees.
const (
	dataTree  = "data"
	cacheTree = "cache"
)

// storagePath is where one host's material sits in a tree, or one app's when an
// app is named. An unnamed host has no folder, and neither has an app of one.
func storagePath(tree, host, app string) string {
	if host == "" {
		return ""
	}
	if app == "" {
		return filepath.Join(configDir(), tree, host)
	}
	return filepath.Join(configDir(), tree, host, app)
}

// storageUsed is what a host or an app occupies across both trees. A host's
// figure covers its apps, because they sit inside it.
func storageUsed(host, app string) int64 {
	return treeSize(storagePath(dataTree, host, app)) +
		treeSize(storagePath(cacheTree, host, app))
}

// treeSize adds up the files under a directory. A directory that was never
// written to does not exist and holds nothing, which is not an error.
func treeSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// foldersTaken is the names already used on disk beneath a host, or at the top
// of both trees when no host is named.
//
// A forgotten client leaves its material behind -- forgetting drops what this
// desktop knows about a client, not what it stored for one -- so a name can be
// spoken for by a folder that no record mentions. Handing that folder to the
// next client whose name cleans the same way would give it somebody else's
// material, so the disk is consulted along with the records.
func foldersTaken(host string) map[string]bool {
	taken := map[string]bool{}
	for _, tree := range []string{dataTree, cacheTree} {
		dir := filepath.Join(configDir(), tree)
		if host != "" {
			dir = storagePath(tree, host, "")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				taken[e.Name()] = true
			}
		}
	}
	return taken
}

// clearCache throws away everything a host or an app has under cache/, leaving
// the data tree untouched. The folder goes with its contents: it is made again
// by whatever next writes into it.
func clearCache(host, app string) error {
	dir := storagePath(cacheTree, host, app)
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

// renameStorage moves a host's folder in both trees, which is what a renamed
// client amounts to on disk. A tree the client has nothing in is left alone
// rather than made empty.
func renameStorage(from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	for _, tree := range []string{dataTree, cacheTree} {
		old := storagePath(tree, from, "")
		if _, err := os.Stat(old); err != nil {
			continue
		}
		if err := os.Rename(old, storagePath(tree, to, "")); err != nil {
			return err
		}
	}
	return nil
}

// The units a size is shown in, and how many bytes each one is.
var storageUnits = []struct {
	suffix string
	scale  int64
}{
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
}

// storageSize is a byte count in a column eight characters wide: 124b, 24K,
// 1.2M. A figure under ten carries one decimal, since 1M and 1.9M are nearly
// twice each other and the difference is worth a character; above that the
// decimal says nothing anyone reads this column for.
//
// Nothing stored reads as nothing at all. A column of 0b down every row is
// noise in the one place the user is looking for what is taking up room.
func storageSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	for _, u := range storageUnits {
		if bytes < u.scale {
			continue
		}
		if scaled := float64(bytes) / float64(u.scale); scaled < 10 {
			return fmt.Sprintf("%.1f%s", scaled, u.suffix)
		}
		return fmt.Sprintf("%d%s", (bytes+u.scale/2)/u.scale, u.suffix)
	}
	return fmt.Sprintf("%db", bytes)
}
