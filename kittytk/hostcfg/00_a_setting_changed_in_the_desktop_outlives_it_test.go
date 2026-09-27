package hostcfg

// The four layers a connection policy can come from, and which one answers.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// ownConfig points the config dir at a throwaway directory, so a test neither
// reads nor writes the developer's own settings, and returns it.
func ownConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(display.PreTrustedOnlyEnv, "")
	t.Setenv(display.PromptLocalEnv, "")
	kittytk := filepath.Join(dir, "kittytk")
	if err := os.MkdirAll(kittytk, 0o700); err != nil {
		t.Fatal(err)
	}
	return kittytk
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Nothing configured anywhere: the built-in answers, and says so.
func TestWithNothingWrittenTheBuiltInAnswers(t *testing.T) {
	ownConfig(t)
	cfg := Load()

	if cfg.ResolvePreTrustedOnly() || cfg.ResolvePromptLocal() {
		t.Errorf("an unconfigured host starts locked down (%v) or asking about "+
			"local clients (%v)", cfg.ResolvePreTrustedOnly(), cfg.ResolvePromptLocal())
	}
	for name, origin := range cfg.PolicyOrigins() {
		if origin != OriginBuiltIn {
			t.Errorf("%s traces back to %q with nothing written anywhere", name, origin)
		}
	}
}

// The ini is read, and named as where the value came from.
func TestTheIniIsWhatAnswersNext(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName), "[service]\npre_trusted_only = true\n")
	cfg := Load()

	if !cfg.ResolvePreTrustedOnly() {
		t.Error("the ini asked for lockdown and did not get it")
	}
	if got := cfg.PolicyOrigins()[PolicyPreTrustedOnly]; got != IniName {
		t.Errorf("the value traces back to %q, want %q", got, IniName)
	}
	if got := cfg.PolicyOrigins()[PolicyPromptLocal]; got != OriginBuiltIn {
		t.Errorf("a policy the ini says nothing about traces back to %q", got)
	}
}

// `current` is read after the ini and overlays it, key by key: what the user
// last did in the desktop wins over what the file was set up with, and a policy
// the overlay says nothing about still comes from the ini.
func TestWhatTheDesktopWroteOverlaysTheIni(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName),
		"[service]\npre_trusted_only = true\nprompt_local = true\n")
	writeFile(t, filepath.Join(dir, CurrentName),
		"[service]\npre_trusted_only = false\n")
	cfg := Load()

	if cfg.ResolvePreTrustedOnly() {
		t.Error("the ini still answers for a policy the user turned off in the desktop")
	}
	if !cfg.ResolvePromptLocal() {
		t.Error("the overlay wiped out a policy it says nothing about")
	}
	origins := cfg.PolicyOrigins()
	if origins[PolicyPreTrustedOnly] != CurrentName {
		t.Errorf("the changed policy traces back to %q", origins[PolicyPreTrustedOnly])
	}
	if origins[PolicyPromptLocal] != IniName {
		t.Errorf("the untouched policy traces back to %q", origins[PolicyPromptLocal])
	}
}

// The environment starts a session: it answers over both files, and names
// itself so the reader knows what to unset.
func TestTheEnvironmentStartsTheSession(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName), "[service]\nprompt_local = false\n")
	writeFile(t, filepath.Join(dir, CurrentName), "[service]\nprompt_local = false\n")
	t.Setenv(display.PromptLocalEnv, "1")

	cfg := Load()
	if !cfg.ResolvePromptLocal() {
		t.Error("the variable was set and the files answered anyway")
	}
	if got := cfg.PolicyOrigins()[PolicyPromptLocal]; got != display.PromptLocalEnv {
		t.Errorf("the value traces back to %q, not the variable that set it", got)
	}
}

// A variable set to nothing is not an answer -- unsetting is how a shell says
// "never mind", and an empty value must read the same way.
func TestAnEmptyVariableIsNotAnAnswer(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName), "[service]\nprompt_local = true\n")
	t.Setenv(display.PromptLocalEnv, "")

	cfg := Load()
	if !cfg.ResolvePromptLocal() {
		t.Error("an empty variable overrode the file")
	}
	if got := cfg.PolicyOrigins()[PolicyPromptLocal]; got != IniName {
		t.Errorf("the value traces back to %q", got)
	}
}

// Changing a policy in the desktop writes the overlay and nothing else: the
// ini the user wrote is left exactly as it was.
func TestKeepingAPolicyWritesOnlyTheOverlay(t *testing.T) {
	dir := ownConfig(t)
	ini := filepath.Join(dir, IniName)
	writeFile(t, ini, "[service]\nprompt_local = true\n")
	before, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}

	if got := KeepPolicy(PolicyPromptLocal, false); got != OriginCurrent {
		t.Fatalf("keeping a policy said it went to %q", got)
	}
	after, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the hand-written ini was rewritten:\n%s", after)
	}

	// And the next start reads the change back.
	if cfg := Load(); cfg.ResolvePromptLocal() {
		t.Error("the change did not survive being written and read again")
	}
}

// The overlay keeps every setting it holds when one of them changes.
func TestOneSettingChangingLeavesTheOthers(t *testing.T) {
	ownConfig(t)
	if got := KeepPolicy(PolicyPreTrustedOnly, true); got != OriginCurrent {
		t.Fatalf("keeping said %q", got)
	}
	if got := KeepPolicy(PolicyPromptLocal, true); got != OriginCurrent {
		t.Fatalf("keeping said %q", got)
	}
	cfg := Load()
	if !cfg.ResolvePreTrustedOnly() || !cfg.ResolvePromptLocal() {
		t.Errorf("writing the second setting lost the first: %+v", cfg.PolicyOrigins())
	}
}

// Clearing a setting takes its line out, so whatever the ini says shows through
// again rather than being permanently masked by a value nobody chose.
func TestClearingASettingLetsTheIniThroughAgain(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName), "[service]\nprompt_local = true\n")
	if _, err := os.Stat(filepath.Join(dir, CurrentName)); err == nil {
		t.Fatal("the overlay exists before anything was changed")
	}

	KeepPolicy(PolicyPromptLocal, false)
	if Load().ResolvePromptLocal() {
		t.Fatal("the change did not take")
	}

	if err := SaveCurrent(CurrentSection, PolicyPromptLocal, ""); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if !cfg.ResolvePromptLocal() {
		t.Error("with the overlay's line gone the ini does not answer again")
	}
	if got := cfg.PolicyOrigins()[PolicyPromptLocal]; got != IniName {
		t.Errorf("the value traces back to %q", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, CurrentName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), PolicyPromptLocal) {
		t.Errorf("the cleared setting is still written down:\n%s", body)
	}
}

// The file says what it is: a program wrote it, and the ini is the one to edit.
func TestTheOverlaySaysWhoWroteIt(t *testing.T) {
	dir := ownConfig(t)
	KeepPolicy(PolicyPromptLocal, true)

	body, err := os.ReadFile(filepath.Join(dir, CurrentName))
	if err != nil {
		t.Fatal(err)
	}
	head := strings.SplitN(string(body), "\n", 2)[0]
	if !strings.HasPrefix(head, ";") {
		t.Fatalf("the file opens with %q, which is not a comment", head)
	}
	if !strings.Contains(string(body), IniName) {
		t.Errorf("the file does not point at the one to edit by hand:\n%s", body)
	}
}

// What a host hands the display service: the resolved values, where they came
// from, and the way back for a change made in the window.
func TestAHostIsGivenTheValuesAndTheWayBack(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName), "[service]\npre_trusted_only = true\n")

	var dcfg display.Config
	Load().ApplyPolicies(&dcfg)

	if !dcfg.PreTrustedOnly {
		t.Error("the server was started without the lockdown the ini asked for")
	}
	if dcfg.PromptLocal {
		t.Error("the server was started asking about local clients")
	}
	if got := dcfg.PolicyOrigins[PolicyPreTrustedOnly]; got != IniName {
		t.Errorf("the origin handed over is %q", got)
	}
	if dcfg.OnPolicyChanged == nil {
		t.Fatal("a change in the window has nowhere to go, so nothing would be kept")
	}
	if got := dcfg.OnPolicyChanged(PolicyPromptLocal, true); got != OriginCurrent {
		t.Errorf("a change was said to be kept in %q", got)
	}
	if !Load().ResolvePromptLocal() {
		t.Error("what the window changed was not there on the next start")
	}
}

// Every host serves the same way -- the terminal one, the graphical one, and
// mew -- so what a user changes in the Connections window is kept whichever of
// them they are looking at. This is that one way.
func TestServingCarriesThePoliciesAndTheWayBack(t *testing.T) {
	dir := ownConfig(t)
	writeFile(t, filepath.Join(dir, IniName),
		"[service]\npre_trusted_only = true\nprompt_local = true\n")

	desktop := trinkets.NewDesktop()
	cfg := Load()
	cfg.Endpoint = filepath.Join(t.TempDir(), "d.sock")
	srv, err := Serve(desktop, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if !srv.PreTrustedOnly() || !srv.PromptLocal() {
		t.Errorf("the server came up at lockdown=%v prompt_local=%v, not where the "+
			"ini put it", srv.PreTrustedOnly(), srv.PromptLocal())
	}
	if got := srv.PolicyOrigin(PolicyPromptLocal); got != IniName {
		t.Errorf("the server traces the value back to %q", got)
	}

	// And the window's own path through the server writes the overlay.
	srv.SetPolicy(PolicyPromptLocal, false)
	if got := srv.PolicyOrigin(PolicyPromptLocal); got != OriginCurrent {
		t.Errorf("after being changed the value traces back to %q", got)
	}
	if Load().ResolvePromptLocal() {
		t.Error("the change was not there to read on the next start")
	}
}
