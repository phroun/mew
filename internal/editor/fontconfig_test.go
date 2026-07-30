package editor

import "testing"

// At startup the editor registers the configured fonts through FontLoader and
// applies the [window] ui_term alias through FontSink, before any painting.
func TestApplyFontConfigAtStartup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.ConfigText = ptrTo(`
[fonts]
JetBrainsMono = /fonts/jbm.ttf

[window]
fonts_path = /extra/fonts
ui_term = "JetBrainsMono, Monday"
`)

	var loadedFiles map[string]string
	var loadedPaths []string
	cfg.FontLoader = func(files map[string]string, paths []string) {
		loadedFiles, loadedPaths = files, paths
	}
	var aliasedTo []string
	var aliasName string
	cfg.FontSink = func(alias string, names []string) bool {
		aliasName, aliasedTo = alias, names
		return true
	}

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })

	if loadedFiles["JetBrainsMono"] != "/fonts/jbm.ttf" {
		t.Errorf("FontLoader files = %v, want JetBrainsMono -> /fonts/jbm.ttf", loadedFiles)
	}
	if len(loadedPaths) != 1 || loadedPaths[0] != "/extra/fonts" {
		t.Errorf("FontLoader searchPaths = %v, want [/extra/fonts]", loadedPaths)
	}
	if aliasName != "ui-term" || len(aliasedTo) != 2 || aliasedTo[0] != "JetBrainsMono" || aliasedTo[1] != "Monday" {
		t.Errorf("FontSink got (%q, %v), want (ui-term, [JetBrainsMono Monday])", aliasName, aliasedTo)
	}
}

// The per-script ui_term_<class> aliases each fire through FontSink at startup.
func TestApplyFontConfigScriptClasses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.ConfigText = ptrTo(`
[window]
ui_term_cjk = "Noto Sans CJK JP"
ui_term_hebrew = "SBL Hebrew"
ui_term_arabic = "Noto Kufi Arabic"
`)
	got := map[string][]string{}
	cfg.FontSink = func(alias string, names []string) bool {
		got[alias] = names
		return true
	}

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })

	for alias, want := range map[string]string{
		"ui-term-cjk": "Noto Sans CJK JP", "ui-term-hebrew": "SBL Hebrew",
		"ui-term-arabic": "Noto Kufi Arabic",
	} {
		if len(got[alias]) != 1 || got[alias][0] != want {
			t.Errorf("FontSink[%q] = %v, want [%q]", alias, got[alias], want)
		}
	}
	if _, fired := got["ui-term"]; fired {
		t.Errorf("ui-term should not fire when only script classes are set")
	}
}

// With no [fonts]/[window] font config, neither sink fires (a plain terminal
// owns its own fonts).
func TestApplyFontConfigNoConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()

	loaderCalled, sinkCalled := false, false
	cfg.FontLoader = func(map[string]string, []string) { loaderCalled = true }
	cfg.FontSink = func(string, []string) bool { sinkCalled = true; return true }

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })

	if loaderCalled {
		t.Error("FontLoader should not fire without [fonts]/fonts_path")
	}
	if sinkCalled {
		t.Error("FontSink should not fire without [window] ui_term")
	}
}
