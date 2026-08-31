package core

import "strings"

// DefaultKeymapConfig is the toolkit's keymap, written in the configuration
// language rather than in Go. DefaultKeyRegistry parses it; hostcfg reads a
// user's [mappings] section with the same reader. There is one keymap
// language, and the default is written in it, so the two cannot drift.
//
// One meaning per line. A key that means several things is written several
// times, and the line order is what the registry's serials record: among
// several keys for one command the LAST wins for display, and among several
// meanings of one key the FIRST a context offers is the one that runs.
const DefaultKeymapConfig = `[mappings]
; Modifier prefixes: C- Control (^ is the same), M- Meta/Alt, S- Shift,
; s- Super/Command. Punctuation may be spelled as a word (Minus, Plus), since
; a bare - separates modifiers. A word in parentheses is an environment hint.
;
; A key written twice means two things, in the order written. An empty command
; unbinds.

; Windows and the desktop. M-F4 and ^Q end the APPLICATION; ^F4 and ^W close
; one WINDOW of it.
M-F10 = window_maximize_toggle
M-F4 = app_quit
^Q = app_quit
^F4 = window_close
^W = window_close

; Both open the menu. F10 is the convention; F2 is there for keyboards where
; F10 is a two-handed press. Menus advertise F10 because it is written last.
F2 = app_menu
F10 = app_menu
F1 = app_help

^H = app_hide
M-^H = app_hide_others
M-^X = desktop_exit

; The clipboard, on whatever has the keyboard.
^X = trinket_cut
^C = trinket_copy
^V = trinket_paste

; Select All also answers to M-a below. The (mac) hint binds s-a everywhere but
; only advertises it on a Mac.
(mac) s-a = trinket_select_all

M-Tab = window_next
M-S-Tab = window_prior
C-Tab = window_mdi_next
C-S-Tab = window_mdi_prior
s-M = app_minimize
s-Minus = gui_scale_down
s-Plus = gui_scale_up
s-0 = gui_scale_reset

Tab = focus_next
S-Tab = focus_prior

Esc = window_cancel_resize
Esc = trinket_cancel

; Return edits where a trinket offers an edit and activates otherwise; Space
; types a space where the trinket takes text and activates otherwise. Space is
; written before Return so menus advertise Return for activation.
;
; Return is the home row's key, not the keypad's. Bind Enter too to give the
; keypad the same meaning.
Space = trinket_type_space
Space = trinket_activate
Return = trinket_edit
Return = trinket_activate

; The bare arrows also carry the fine resize, which is a splitter's step.
Up = window_move_fine_up
Up = window_size_fine_up
Up = trinket_item_up
Up = trinket_item_prior
Down = window_move_fine_down
Down = window_size_fine_down
Down = trinket_item_down
Down = trinket_item_next
Left = window_move_fine_left
Left = window_size_fine_left
Left = trinket_item_left
Left = trinket_item_prior
Right = window_move_fine_right
Right = window_size_fine_right
Right = trinket_item_right
Right = trinket_item_next

S-Up = window_size_fine_up
S-Up = trinket_sel_up
S-Up = terminal_scroll_up
S-Down = window_size_fine_down
S-Down = trinket_sel_down
S-Down = terminal_scroll_down

; The shifted left/right also carry the classic tree walk, so an editable grid
; can spend its plain arrows on the edit-target column.
S-Left = window_size_fine_left
S-Left = trinket_sel_left
S-Left = trinket_collapse_or_enclosing
S-Left = trinket_item_left
S-Right = window_size_fine_right
S-Right = trinket_sel_right
S-Right = trinket_expand_or_descend
S-Right = trinket_item_right

Home = trinket_beg
End = trinket_end
S-Home = trinket_sel_beg
S-Home = terminal_scroll_beg
S-End = trinket_sel_end
S-End = terminal_scroll_end

PageUp = trinket_page_prior
PageDown = trinket_page_next
S-PageUp = terminal_scroll_page_prior
S-PageDown = terminal_scroll_page_next

; Editing. "Delete" is the DEL character and erases behind, like Backspace;
; "FDel" is the key that erases ahead. Terminals send one of the first two.
Backspace = trinket_del_prior
Backspace = trinket_enclosing
Delete = trinket_del_prior
Delete = trinket_enclosing
FDel = trinket_del_next
^U = trinket_del_line

; ^A goes to the beginning, and selects all when the caret is already there
; with nothing selected. A trinket that does not offer that gets the plain move.
^A = trinket_beg_or_select_all
^A = trinket_beg
^E = trinket_end
S-^A = trinket_sel_beg
S-^E = trinket_sel_end
M-a = trinket_select_all

; Trees.
Plus = trinket_expand
Minus = trinket_collapse
Asterisk = trinket_expand_all
Slash = trinket_collapse_all

; Dropping a combo box open.
F4 = trinket_open

; The coarse window move and size, and a splitter's big step.
C-Up = window_move_up
C-Up = window_size_up
C-Up = trinket_scroll_up
C-Up = trinket_beg
M-Up = window_move_up
M-Up = window_size_up
M-Up = trinket_scroll_up
M-Up = trinket_open
M-Up = trinket_beg
s-Up = window_move_up
C-Down = window_move_down
C-Down = window_size_down
C-Down = trinket_scroll_down
C-Down = trinket_end
M-Down = window_move_down
M-Down = window_size_down
M-Down = trinket_scroll_down
M-Down = trinket_open
M-Down = trinket_end
s-Down = window_move_down
C-Left = window_move_left
C-Left = window_size_left
C-Left = trinket_beg
M-Left = window_move_left
M-Left = window_size_left
M-Left = trinket_beg
s-Left = window_move_left
C-Right = window_move_right
C-Right = window_size_right
C-Right = trinket_end
M-Right = window_move_right
M-Right = window_size_right
M-Right = trinket_end
s-Right = window_move_right

C-S-Up = window_size_up
M-S-Up = window_size_up
S-s-Up = window_size_up
C-S-Down = window_size_down
M-S-Down = window_size_down
S-s-Down = window_size_down
C-S-Left = window_size_left
M-S-Left = window_size_left
S-s-Left = window_size_left
C-S-Right = window_size_right
M-S-Right = window_size_right
S-s-Right = window_size_right

; Ctrl+PageUp/Down pages a scrolling trinket and cycles MDI tabs otherwise.
C-PageUp = trinket_page_prior
C-PageUp = window_mdi_prior
C-PageDown = trinket_page_next
C-PageDown = window_mdi_next
`

// ParseKeymap reads keymap text into bindings, one per line, in the order
// written: the default above, or a host's [mappings] section.
//
// Format is the ini one — "key = command", ; and # comments, blank lines
// ignored, a [mappings] header tolerated and any other section ending the
// keymap. The key keeps its case, being data rather than a setting name:
// "S-Tab" and "s-Tab" are Shift-Tab and Super-Tab. A line with an empty
// command is kept so callers can act on it; that is how a file unbinds.
func ParseKeymap(text string) []Binding {
	var out []Binding
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				if strings.ToLower(strings.TrimSpace(line[1:end])) != "mappings" {
					break
				}
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		cmd := stripKeymapQuotes(strings.TrimSpace(stripKeymapComment(line[eq+1:])))
		out = append(out, Binding{Key: key, Commands: []string{cmd}})
	}
	return out
}

// stripKeymapComment removes a trailing ; or # comment. It must be preceded by
// whitespace, so a command containing one is left alone.
func stripKeymapComment(v string) string {
	for i := 0; i < len(v); i++ {
		if c := v[i]; c == ';' || c == '#' {
			if i == 0 || v[i-1] == ' ' || v[i-1] == '\t' {
				return v[:i]
			}
		}
	}
	return v
}

// stripKeymapQuotes removes a matching pair of surrounding quotes.
func stripKeymapQuotes(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}
