package display

// When each client last spoke to this display server.
//
// A stamp is written when a connection is admitted, which is the moment a
// client has both proved who it is and been allowed to talk. Like a nickname
// it decides nothing: losing the file loses the answer to "when was this
// last used", and nothing else.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SeenStoreEnv overrides the path of the last-seen file.
const SeenStoreEnv = "KITTYTK_LAST_SEEN"

func seenStorePath() string {
	if p := os.Getenv(SeenStoreEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "last_seen")
}

func newSeenStore(path string) *pairStore {
	if path == "" {
		path = seenStorePath()
	}
	return newPairStore(path, "# KittyTK connection last-seen times: <identity> <RFC 3339>")
}

// markSeen records that a client was admitted at this moment. The stamp is
// kept to the second although the window shows the day: a stamp that has
// thrown away everything but the day cannot be asked a finer question later.
func markSeen(s *pairStore, identity string, at time.Time) error {
	if s == nil || identity == "" {
		return nil
	}
	return s.set(identity, at.UTC().Format(time.RFC3339))
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
