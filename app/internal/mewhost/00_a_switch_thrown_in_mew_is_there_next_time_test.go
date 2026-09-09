package mewhost

// mew reads its own launch configuration rather than kittytk.ini, so the
// desktop's `current` overlay has to be asked for by name here. Without it a
// switch thrown in the Connections window would be written and never read
// back, and mew alone would forget what every other host remembers.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
)

// ownHome points the home and config directories at throwaway ones, so a test
// neither reads nor writes the developer's own settings.
func ownHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(display.PreTrustedOnlyEnv, "")
	t.Setenv(display.PromptLocalEnv, "")
	if err := os.MkdirAll(filepath.Join(home, ".mew"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func writeEditorConf(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, ".mew", "editor.conf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// editor.conf carries the two connection policies, and says so where the
// window accounts for them.
func TestEditorConfCarriesTheConnectionPolicies(t *testing.T) {
	home := ownHome(t)
	writeEditorConf(t, home, "[service]\npre_trusted_only = true\nprompt_local = yes\n")

	cfg := LoadHostConfig()
	if !cfg.ResolvePreTrustedOnly() || !cfg.ResolvePromptLocal() {
		t.Fatalf("editor.conf asked for lockdown=%v prompt_local=%v and got neither",
			cfg.ResolvePreTrustedOnly(), cfg.ResolvePromptLocal())
	}
	for name, origin := range cfg.PolicyOrigins() {
		if origin != "editor.conf" {
			t.Errorf("%s traces back to %q, not the file that set it", name, origin)
		}
	}
}

// And what the user last changed in the Connections window overlays it, which
// is the whole point: mew is a host like the others.
func TestWhatTheWindowWroteReachesMew(t *testing.T) {
	home := ownHome(t)
	writeEditorConf(t, home, "[service]\nprompt_local = true\n")
	if !LoadHostConfig().ResolvePromptLocal() {
		t.Fatal("editor.conf did not take")
	}

	// The window's own path: the server keeps the change wherever the host says.
	if got := hostcfg.KeepPolicy(hostcfg.PolicyPromptLocal, false); got != hostcfg.OriginCurrent {
		t.Fatalf("the change was said to be kept in %q", got)
	}

	cfg := LoadHostConfig()
	if cfg.ResolvePromptLocal() {
		t.Error("mew came back up doing what editor.conf said, not what the user " +
			"last chose in the window")
	}
	if got := cfg.PolicyOrigins()[hostcfg.PolicyPromptLocal]; got != hostcfg.OriginCurrent {
		t.Errorf("the value traces back to %q", got)
	}
}

// A policy neither file mentions is still the built-in, and says so.
func TestAPolicyNobodyWroteIsTheBuiltIn(t *testing.T) {
	ownHome(t)
	cfg := LoadHostConfig()
	if cfg.ResolvePreTrustedOnly() || cfg.ResolvePromptLocal() {
		t.Error("an unconfigured mew starts somewhere other than the built-in")
	}
	if got := cfg.PolicyOrigins()[hostcfg.PolicyPreTrustedOnly]; got != hostcfg.OriginBuiltIn {
		t.Errorf("the value traces back to %q", got)
	}
}
