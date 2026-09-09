package display

// A file of one line per identity: the identity, a space, and the rest of the
// line as its value.
//
// Two of these sit beside the authorizations store -- the names the user gives
// the clients that connect here, and when each of them last did -- and neither
// takes part in deciding anything. Unlike the authorizations store, which is
// appended to and read with deny winning, these are rewritten whole: a second
// value for an identity replaces the first rather than sitting behind it.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type pairStore struct {
	path   string
	header string // the comment line written above the pairs
	mu     sync.Mutex
}

func newPairStore(path, header string) *pairStore {
	return &pairStore{path: path, header: header}
}

// all reads every pair. A missing file is an empty set, not an error: a host
// nobody has named or heard from yet is the ordinary case.
func (s *pairStore) all() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *pairStore) readLocked() map[string]string {
	out := map[string]string{}
	f, err := os.Open(s.path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		id, value, ok := parsePairLine(sc.Text())
		if ok {
			out[id] = value
		}
	}
	return out
}

// get is the value for one identity, or "" when it has none.
func (s *pairStore) get(identity string) string {
	return s.all()[identity]
}

// set records a value, or removes the identity when the value is blank. The
// whole file is rewritten so a second value replaces rather than accumulates.
func (s *pairStore) set(identity, value string) error {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	value = strings.TrimSpace(value)

	s.mu.Lock()
	defer s.mu.Unlock()
	pairs := s.readLocked()
	if value == "" {
		delete(pairs, identity)
	} else {
		pairs[identity] = value
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	ids := make([]string, 0, len(pairs))
	for id := range pairs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var sb strings.Builder
	sb.WriteString(s.header + "\n")
	for _, id := range ids {
		fmt.Fprintf(&sb, "%s %s\n", id, pairs[id])
	}
	return os.WriteFile(s.path, []byte(sb.String()), 0o600)
}

// parsePairLine splits a stored line. The value is everything after the
// identity, so it may hold spaces; blank lines and comments are skipped.
func parsePairLine(line string) (identity, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
	if i < 0 {
		return "", "", false
	}
	identity = line[:i]
	value = strings.TrimSpace(line[i:])
	if identity == "" || value == "" {
		return "", "", false
	}
	return identity, value, true
}
