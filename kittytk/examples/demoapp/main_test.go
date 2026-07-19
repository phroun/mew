package main

import (
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// Every script the demoapp sends over the socket must be well-formed
// protocol text. Parsing them here catches malformed builds without a
// running display service.
func TestScriptsParse(t *testing.T) {
	scripts := map[string]string{
		"mainBuild":      mainBuildScript(),
		"mainMenu":       mainMenuScript(),
		"mainStatus":     mainStatusScript,
		"protocolWindow": protocolWindowScript,
		"aboutDialog":    aboutDialogScript,
		"demoTerminal":   demoTerminalScript(1),
		"secondaryBuild": secondaryBuildScript(1),
		"mdiChild":       mdiChildScript(1),
	}
	for name, src := range scripts {
		if _, err := protocol.Parse(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
