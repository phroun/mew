package display

// Names a user gives the clients that connect here.
//
// The authorizations store knows a peer by its certificate fingerprint, which
// is exact and unreadable. A nickname is what the user calls it. Nothing
// depends on one: lose this file and every standing decision is untouched,
// because a nickname never takes part in deciding anything.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// NicknameStoreEnv overrides the path of the nickname file.
const NicknameStoreEnv = "KITTYTK_NICKNAMES"

func nicknameStorePath() string {
	if p := os.Getenv(NicknameStoreEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "nicknames")
}

// nicknameStore is the persistent identity-to-name record. Unlike the
// authorizations store it is rewritten rather than appended, so renaming a
// peer replaces its name instead of leaving the old one behind it.
type nicknameStore struct {
	path string
	mu   sync.Mutex
}

func newNicknameStore(path string) *nicknameStore {
	if path == "" {
		path = nicknameStorePath()
	}
	return &nicknameStore{path: path}
}

// all reads every name. A missing file is an empty set, not an error: a host
// nobody has named yet is the ordinary case.
func (s *nicknameStore) all() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *nicknameStore) readLocked() map[string]string {
	out := map[string]string{}
	f, err := os.Open(s.path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		id, name, ok := parseNicknameLine(sc.Text())
		if ok {
			out[id] = name
		}
	}
	return out
}

// get is the name for one identity, or "" when it has none.
func (s *nicknameStore) get(identity string) string {
	return s.all()[identity]
}

// set records a name, or removes it when name is blank. The whole file is
// rewritten so a rename replaces rather than accumulates.
func (s *nicknameStore) set(identity, name string) error {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	name = strings.TrimSpace(name)

	s.mu.Lock()
	defer s.mu.Unlock()
	names := s.readLocked()
	if name == "" {
		delete(names, identity)
	} else {
		names[identity] = name
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var sb strings.Builder
	sb.WriteString("# KittyTK connection nicknames: <identity> <name>\n")
	for _, id := range ids {
		fmt.Fprintf(&sb, "%s %s\n", id, names[id])
	}
	return os.WriteFile(s.path, []byte(sb.String()), 0o600)
}

// parseNicknameLine splits a stored line. The name is everything after the
// identity, so it may hold spaces; blank lines and comments are skipped.
func parseNicknameLine(line string) (identity, name string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
	if i < 0 {
		return "", "", false
	}
	identity = line[:i]
	name = strings.TrimSpace(line[i:])
	if identity == "" || name == "" {
		return "", "", false
	}
	return identity, name, true
}
