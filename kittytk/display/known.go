package display

// Who this desktop has let in, and what it calls them on disk.
//
// The authorizations store holds DECISIONS: a client with no standing rule has
// no line in it. But a client admitted once only is still a client that has
// been here, and it has a name, a date and a folder of its own. This is where
// that is written, one record per host and one per app beneath it.
//
// A record is made when a connection is admitted, so being listed means having
// got in at least once. A connection turned away for the moment writes nothing
// and leaves no trace; one turned away for good is a rule, and the
// authorizations store carries that.
//
// Like the nickname file, nothing here decides anything: losing it loses the
// dates and orphans the folders, and every standing decision is untouched.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KnownStoreEnv overrides the path of the known-clients file.
const KnownStoreEnv = "KITTYTK_KNOWN"

func knownStorePath() string {
	if p := os.Getenv(KnownStoreEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "known")
}

// knownRecord is one host, or one app of one host.
type knownRecord struct {
	identity string // the client this is about
	app      string // the app's own name; empty on a host's record
	safe     string // what it is called on disk
	seen     string // RFC 3339, or empty for a record made without a visit
}

// knownStore is the file, read and rewritten whole. A record is replaced in
// place rather than appended to, so a client that has connected a thousand
// times has one line.
type knownStore struct {
	path string
	mu   sync.Mutex
}

func newKnownStore(path string) *knownStore {
	if path == "" {
		path = knownStorePath()
	}
	return &knownStore{path: path}
}

const knownHeader = "# KittyTK known clients: host <identity> <safe name> <last seen>" +
	" | app <identity> <safe name> <last seen> <app name>"

// all reads every record, in the order the file holds them -- which is the
// order they were first admitted in.
func (s *knownStore) all() []knownRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *knownStore) readLocked() []knownRecord {
	f, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []knownRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if r, ok := parseKnownLine(sc.Text()); ok {
			out = append(out, r)
		}
	}
	return out
}

func (s *knownStore) writeLocked(records []knownRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString(knownHeader + "\n")
	for _, r := range records {
		stamp := r.seen
		if stamp == "" {
			stamp = "-"
		}
		if r.app == "" {
			fmt.Fprintf(&sb, "host %s %s %s\n", r.identity, r.safe, stamp)
			continue
		}
		fmt.Fprintf(&sb, "app %s %s %s %s\n", r.identity, r.safe, stamp, r.app)
	}
	return os.WriteFile(s.path, []byte(sb.String()), 0o600)
}

// parseKnownLine reads one stored line. The app's name is the remainder of the
// line, so it may hold spaces; the three fields before it cannot.
func parseKnownLine(line string) (knownRecord, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return knownRecord{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return knownRecord{}, false
	}
	r := knownRecord{identity: fields[1], safe: fields[2], seen: fields[3]}
	if r.seen == "-" {
		r.seen = ""
	}
	if r.identity == "" || r.safe == "" {
		return knownRecord{}, false
	}
	switch fields[0] {
	case "host":
		return r, true
	case "app":
		// Walk past the four fields rather than searching for the last of
		// them: a stamp of "-" would otherwise be found inside the safe name.
		at := 0
		for _, f := range fields[:4] {
			at += strings.Index(line[at:], f) + len(f)
		}
		r.app = strings.TrimSpace(line[at:])
		return r, r.app != ""
	}
	return knownRecord{}, false
}

// admitted records that a client got in, under the nickname it has now. Both
// the host and the app it connected as are stamped, and either that has never
// been seen before is given the name it will be filed under.
func (s *knownStore) admitted(identity, app, nickname string, at time.Time) error {
	if identity == "" {
		return nil
	}
	stamp := at.UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.readLocked()

	records = stamped(records, identity, "", nickname, unnamedHost, stamp)
	if app != "" {
		records = stamped(records, identity, app, app, unnamedApp, stamp)
	}
	return s.writeLocked(records)
}

// stamped puts the moment on one record, adding it with a name of its own if
// this is the first anyone has heard of it.
func stamped(records []knownRecord, identity, app, name, fallback, stamp string) []knownRecord {
	for i := range records {
		if records[i].identity == identity && records[i].app == app {
			records[i].seen = stamp
			return records
		}
	}
	return append(records, knownRecord{
		identity: identity,
		app:      app,
		safe:     safeName(name, fallback, namesTaken(records, identity, app)),
		seen:     stamp,
	})
}

// namesTaken is what a new record may not be called. A host is named among all
// the hosts, since their folders sit side by side; an app is named among its
// own host's apps, since its folder sits inside that host's. Folders already on
// disk count too, since a folder is the thing the name is for.
func namesTaken(records []knownRecord, identity, app string) map[string]bool {
	taken := map[string]bool{}
	if app == "" {
		taken = foldersTaken("")
	} else if host := hostSafe(records, identity); host != "" {
		taken = foldersTaken(host)
	}
	for _, r := range records {
		switch {
		case app == "" && r.app == "":
			taken[r.safe] = true
		case app != "" && r.app != "" && r.identity == identity:
			taken[r.safe] = true
		}
	}
	return taken
}

// folders is where one app of one client keeps its material: the client's
// folder, and the app's within it. Either coming back empty means there is no
// folder -- a peer with no identity, or one this desktop has not admitted.
func (s *knownStore) folders(identity, app string) (host, item string) {
	records := s.all()
	for _, r := range records {
		if r.identity == identity && r.app == app && app != "" {
			item = r.safe
		}
	}
	return hostSafe(records, identity), item
}

// hostSafe is the folder one client keeps its material in, or empty for a
// client with no record.
func hostSafe(records []knownRecord, identity string) string {
	for _, r := range records {
		if r.identity == identity && r.app == "" {
			return r.safe
		}
	}
	return ""
}

// rename gives a host the name its new nickname cleans down to, and answers
// with the folder name it had and the one it has now. They come back equal when
// the nickname cleans to what it was already called, or when the client is one
// this desktop has never admitted and so has no folder to move.
func (s *knownStore) rename(identity, nickname string) (from, to string, err error) {
	if identity == "" {
		return "", "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.readLocked()

	at := -1
	for i, r := range records {
		if r.identity == identity && r.app == "" {
			at = i
			break
		}
	}
	if at < 0 {
		return "", "", nil
	}
	from = records[at].safe

	// The name it holds now is not in its own way: renaming a client to what
	// it is already called must not number it.
	taken := namesTaken(records, identity, "")
	delete(taken, from)
	to = safeName(nickname, unnamedHost, taken)
	if to == from {
		return from, from, nil
	}
	records[at].safe = to
	return from, to, s.writeLocked(records)
}

// forget drops a client and every app beneath it.
func (s *knownStore) forget(identity string) error {
	return s.drop(func(r knownRecord) bool { return r.identity == identity })
}

// forgetApp drops one app of one client.
func (s *knownStore) forgetApp(identity, app string) error {
	return s.drop(func(r knownRecord) bool {
		return r.identity == identity && r.app == app
	})
}

func (s *knownStore) drop(match func(knownRecord) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []knownRecord
	for _, r := range s.readLocked() {
		if !match(r) {
			out = append(out, r)
		}
	}
	return s.writeLocked(out)
}

// seenDate is the day a stamp names, read in the zone the user is in. A stamp
// that cannot be read is no date rather than a wrong one.
func seenDate(stamp string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(stamp))
	if err != nil {
		return ""
	}
	return t.Local().Format("2006-01-02")
}
