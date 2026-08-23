package core

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// dumpRegistry renders a registry's whole table as text so two of them can be
// compared exactly: every key, and for each the commands in the order they
// resolve, with the weight and serial that decide what gets advertised.
func dumpRegistry(r *KeyRegistry) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.bindings))
	for k := range r.bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, bc := range r.bindings[k] {
			fmt.Fprintf(&b, "%s\t%s\tw=%d\ts=%d\n", k, bc.command, bc.weight, bc.serial)
		}
	}
	return b.String()
}

func diffDumps(t *testing.T, want, got string) {
	t.Helper()
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	shown := 0
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		t.Errorf("line %d:\n  want %q\n  got  %q", i+1, wl, gl)
		if shown++; shown == 12 {
			t.Errorf("... and possibly more")
			return
		}
	}
}

// The default keymap in DefaultKeymapConfig and the default registry are the
// same table, down to the serials — which is the point of writing it in the
// configuration language: what the toolkit binds by default is exactly what a
// file could have said, so the two cannot drift apart.
//
// Before this, the default lived in a Go literal of Binding values and the
// shipped kittytk.ini carried a hand-written [mappings] section claiming to be
// the same thing. It was not: the file still said "Space = trinket_activate"
// long after the table had grown a trinket_type_space ahead of it, and a user
// running with the shipped file got the space bar bug back.
func TestDefaultKeymapConfigIsTheDefaultRegistry(t *testing.T) {
	parsed := ParseKeymap(DefaultKeymapConfig)
	if len(parsed) == 0 {
		t.Fatal("the default keymap parsed to nothing")
	}
	got := dumpRegistry(NewKeyRegistry("from-config", parsed))
	want := dumpRegistry(NewKeyRegistry("reference", referenceDefaultBindings))
	if got != want {
		diffDumps(t, want, got)
	}
}

// One line, one meaning, in the order written -- including a key written more
// than once, which is how the configuration language spells a key that means
// several things.
func TestParseKeymapReadsOneMeaningPerLine(t *testing.T) {
	got := ParseKeymap(`[mappings]
; a comment
Space = trinket_type_space
Space = trinket_activate   ; and a trailing one

^Q =
"S-Tab" = focus_prior
`)
	want := []Binding{
		{"Space", []string{"trinket_type_space"}},
		{"Space", []string{"trinket_activate"}},
		{"^Q", []string{""}},
		{`"S-Tab"`, []string{"focus_prior"}},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Key != want[i].Key || got[i].Commands[0] != want[i].Commands[0] {
			t.Errorf("line %d = %q -> %q, want %q -> %q",
				i+1, got[i].Key, got[i].Commands[0], want[i].Key, want[i].Commands[0])
		}
	}
}

// A section that is not [mappings] ends the keymap, so the reader can be
// handed a slice of a larger file without swallowing the next section.
func TestParseKeymapStopsAtAnotherSection(t *testing.T) {
	got := ParseKeymap("[mappings]\nTab = focus_next\n[window]\nwidth = 1024\n")
	if len(got) != 1 || got[0].Key != "Tab" {
		t.Fatalf("parsed %+v, want just the Tab line", got)
	}
}

// referenceDefaultBindings is the Go table the default keymap used to be,
// kept HERE, in the test, as the thing DefaultKeymapConfig is measured
// against. It is not the source of truth any more -- the configuration text
// is -- but a transcription needs something to be checked against, and a
// second independent statement of the table is what catches a typo in it.
var referenceDefaultBindings = []Binding{
	{"M-F10", []string{CmdWindowMaximizeToggle}},
	{"M-F4", []string{CmdAppQuit}},
	{"^Q", []string{CmdAppQuit}},
	{"^F4", []string{CmdWindowClose}},
	{"^W", []string{CmdWindowClose}},
	{"F2", []string{CmdAppMenu}},
	{"F10", []string{CmdAppMenu}},
	{"F1", []string{CmdAppHelp}},
	{"^H", []string{CmdAppHide}},
	{"M-^H", []string{CmdAppHideOthers}},
	{"M-^X", []string{CmdDesktopExit}},
	{"^X", []string{CmdTrinketCut}},
	{"^C", []string{CmdTrinketCopy}},
	{"^V", []string{CmdTrinketPaste}},
	{"(mac) s-a", []string{CmdTrinketSelectAll}},
	{"M-Tab", []string{CmdWindowNext}},
	{"M-S-Tab", []string{CmdWindowPrior}},
	{"C-Tab", []string{CmdWindowMDINext}},
	{"C-S-Tab", []string{CmdWindowMDIPrior}},
	{"s-M", []string{CmdAppMinimize}},
	{"s-Minus", []string{CmdGUIScaleDown}},
	{"s-Plus", []string{CmdGUIScaleUp}},
	{"s-0", []string{CmdGUIScaleReset}},

	{"Tab", []string{CmdFocusNext}},
	{"S-Tab", []string{CmdFocusPrior}},

	{"Esc", []string{CmdWindowCancelResize, CmdTrinketCancel}},
	{"Space", []string{CmdTrinketTypeSpace, CmdTrinketActivate}},
	{"Return", []string{CmdTrinketEdit, CmdTrinketActivate}},

	{"Up", []string{CmdWindowMoveFineUp, CmdWindowSizeFineUp, CmdTrinketItemUp, CmdTrinketItemPrior}},
	{"Down", []string{CmdWindowMoveFineDown, CmdWindowSizeFineDown, CmdTrinketItemDown, CmdTrinketItemNext}},
	{"Left", []string{CmdWindowMoveFineLeft, CmdWindowSizeFineLeft, CmdTrinketItemLeft, CmdTrinketItemPrior}},
	{"Right", []string{CmdWindowMoveFineRight, CmdWindowSizeFineRight, CmdTrinketItemRight, CmdTrinketItemNext}},

	{"S-Up", []string{CmdWindowSizeFineUp, CmdTrinketSelUp, CmdTerminalScrollUp}},
	{"S-Down", []string{CmdWindowSizeFineDown, CmdTrinketSelDown, CmdTerminalScrollDown}},
	{"S-Left", []string{CmdWindowSizeFineLeft, CmdTrinketSelLeft, CmdTrinketCollapseOrEnclosing, CmdTrinketItemLeft}},
	{"S-Right", []string{CmdWindowSizeFineRight, CmdTrinketSelRight, CmdTrinketExpandOrDescend, CmdTrinketItemRight}},

	{"Home", []string{CmdTrinketBeg}},
	{"End", []string{CmdTrinketEnd}},
	{"S-Home", []string{CmdTrinketSelBeg, CmdTerminalScrollBeg}},
	{"S-End", []string{CmdTrinketSelEnd, CmdTerminalScrollEnd}},

	{"PageUp", []string{CmdTrinketPagePrior}},
	{"PageDown", []string{CmdTrinketPageNext}},
	{"S-PageUp", []string{CmdTerminalScrollPagePrior}},
	{"S-PageDown", []string{CmdTerminalScrollPageNext}},

	{"Backspace", []string{CmdTrinketDelPrior, CmdTrinketEnclosing}},
	{"Delete", []string{CmdTrinketDelPrior, CmdTrinketEnclosing}},
	{"FDel", []string{CmdTrinketDelNext}},
	{"^U", []string{CmdTrinketDelLine}},
	{"^A", []string{CmdTrinketBegOrSelectAll, CmdTrinketBeg}},
	{"^E", []string{CmdTrinketEnd}},
	{"S-^A", []string{CmdTrinketSelBeg}},
	{"S-^E", []string{CmdTrinketSelEnd}},
	{"M-a", []string{CmdTrinketSelectAll}},

	{"Plus", []string{CmdTrinketExpand}},
	{"Minus", []string{CmdTrinketCollapse}},
	{"Asterisk", []string{CmdTrinketExpandAll}},
	{"Slash", []string{CmdTrinketCollapseAll}},

	{"F4", []string{CmdTrinketOpen}},

	{"C-Up", []string{CmdWindowMoveUp, CmdWindowSizeUp, CmdTrinketScrollUp, CmdTrinketBeg}},
	{"M-Up", []string{CmdWindowMoveUp, CmdWindowSizeUp, CmdTrinketScrollUp, CmdTrinketOpen, CmdTrinketBeg}},
	{"s-Up", []string{CmdWindowMoveUp}},
	{"C-Down", []string{CmdWindowMoveDown, CmdWindowSizeDown, CmdTrinketScrollDown, CmdTrinketEnd}},
	{"M-Down", []string{CmdWindowMoveDown, CmdWindowSizeDown, CmdTrinketScrollDown, CmdTrinketOpen, CmdTrinketEnd}},
	{"s-Down", []string{CmdWindowMoveDown}},
	{"C-Left", []string{CmdWindowMoveLeft, CmdWindowSizeLeft, CmdTrinketBeg}},
	{"M-Left", []string{CmdWindowMoveLeft, CmdWindowSizeLeft, CmdTrinketBeg}},
	{"s-Left", []string{CmdWindowMoveLeft}},
	{"C-Right", []string{CmdWindowMoveRight, CmdWindowSizeRight, CmdTrinketEnd}},
	{"M-Right", []string{CmdWindowMoveRight, CmdWindowSizeRight, CmdTrinketEnd}},
	{"s-Right", []string{CmdWindowMoveRight}},

	{"C-S-Up", []string{CmdWindowSizeUp}},
	{"M-S-Up", []string{CmdWindowSizeUp}},
	{"S-s-Up", []string{CmdWindowSizeUp}},
	{"C-S-Down", []string{CmdWindowSizeDown}},
	{"M-S-Down", []string{CmdWindowSizeDown}},
	{"S-s-Down", []string{CmdWindowSizeDown}},
	{"C-S-Left", []string{CmdWindowSizeLeft}},
	{"M-S-Left", []string{CmdWindowSizeLeft}},
	{"S-s-Left", []string{CmdWindowSizeLeft}},
	{"C-S-Right", []string{CmdWindowSizeRight}},
	{"M-S-Right", []string{CmdWindowSizeRight}},
	{"S-s-Right", []string{CmdWindowSizeRight}},

	{"C-PageUp", []string{CmdTrinketPagePrior, CmdWindowMDIPrior}},
	{"C-PageDown", []string{CmdTrinketPageNext, CmdWindowMDINext}},
}

// dumpCommands renders every key's meanings in resolution order, ignoring the
// serials -- which necessarily move when a file rebinds, and are not what
// "the same table" means here.
func dumpCommands(r *KeyRegistry) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.bindings))
	for k := range r.bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		for _, bc := range r.bindings[k] {
			b.WriteString(" " + bc.command)
		}
		b.WriteString("\n")
	}
	return b.String()
}
