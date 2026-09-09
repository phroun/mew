package display

// Names a user gives the clients that connect here.
//
// The authorizations store knows a peer by its certificate fingerprint, which
// is exact and unreadable. A nickname is what the user calls it. Nothing
// depends on one: lose this file and every standing decision is untouched,
// because a nickname never takes part in deciding anything.

import (
	"os"
	"path/filepath"
)

// NicknameStoreEnv overrides the path of the nickname file.
const NicknameStoreEnv = "KITTYTK_NICKNAMES"

func nicknameStorePath() string {
	if p := os.Getenv(NicknameStoreEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "nicknames")
}

func newNicknameStore(path string) *pairStore {
	if path == "" {
		path = nicknameStorePath()
	}
	return newPairStore(path, "# KittyTK connection nicknames: <identity> <name>")
}
