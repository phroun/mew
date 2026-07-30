#!/usr/bin/env bash
# Terminal probe: pointed-Hebrew selection styling under a bidi-applying
# terminal (macOS Terminal.app — the flipBidiForHost case).
#
# BACKGROUND: mew's flip emission for a selected pointed-Hebrew row is
# provably per-glyph correct on the wire (render test
# TestFlipPointedHebrewSelectionWire), yet Terminal.app draws the selection
# bar displaced and ~half width, with occasional wrong-colored letters —
# while mark-free Arabic selections render perfectly. This probe prints the
# SAME selected sentence in several candidate wire encodings so the
# terminal itself reveals which encoding it styles correctly.
#
# RUN in the terminal under test (no mew involved):   bash hebrew-selection-probe.sh
#
# WHAT TO REPORT, per row A-E:
#   1. Does the sentence read correctly (same as row A's text)?
#   2. Is the white selection bar exactly under the middle words
#      (the "want to drink" span), with every letter on it black?
#   3. Any letters in the wrong color (green inside the bar / black outside)?
#
# The sentence (logical):  אֲנִי רוֹצֶה לִשְׁתּוֹת מַיִם.
# The selected span:       from the vav-holam of רוֹצֶה through the final ת
#                          of לִשְׁתּוֹת (9 cells, interior space included).

G=$'\x1b[0;32;40m'   # green on black (the file's comment color)
S=$'\x1b[0;30;47m'   # black on white (mew's selection color)
R=$'\x1b[0m'
LRO=$'\xe2\x80\xad'  # U+202D LEFT-TO-RIGHT OVERRIDE
PDF=$'\xe2\x80\xac'  # U+202C POP DIRECTIONAL FORMATTING

# Logical clusters (base + its niqqud), index 0..18.
CL=("אֲ" "נִ" "י" " " "ר" "וֹ" "צֶ" "ה" " " "לִ" "שְׁ" "תּ" "וֹ" "ת" " " "מַ" "יִ" "ם" ".")
# Bare variants (marks stripped) for the control row.
BARE=("א" "נ" "י" " " "ר" "ו" "צ" "ה" " " "ל" "ש" "ת" "ו" "ת" " " "מ" "י" "ם" ".")
SEL_FROM=5
SEL_TO=13

style_of() { # cluster index -> SGR
  if [ "$1" -ge $SEL_FROM ] && [ "$1" -le $SEL_TO ]; then printf '%s' "$S"; else printf '%s' "$G"; fi
}

echo
echo "== pointed-Hebrew selection probe =="
echo "look for: white bar EXACTLY under the middle words, all bar letters black"
echo

# A) mew's CURRENT flip wire: logical order, per-glyph SGR, marks after base.
printf 'A) '
for i in $(seq 0 18); do printf '%s%s' "$(style_of "$i")" "${CL[$i]}"; done
printf '%s\n' "$R"

# B) logical order, SGR only at style CHANGES (coalesced spans).
printf 'B) %s' "$G"
for i in $(seq 0 18); do
  if [ "$i" -eq $SEL_FROM ]; then printf '%s' "$S"; fi
  if [ "$i" -eq $((SEL_TO + 1)) ]; then printf '%s' "$G"; fi
  printf '%s' "${CL[$i]}"
done
printf '%s\n' "$R"

# C) VISUAL order wrapped in LRO..PDF (terminal reorder suppressed),
#    per-glyph SGR. Marks still follow their base in the byte stream.
printf 'C) %s' "$LRO"
for i in $(seq 18 -1 0); do printf '%s%s' "$(style_of "$i")" "${CL[$i]}"; done
printf '%s%s\n' "$PDF" "$R"

# D) control: logical order, per-glyph SGR, MARKS STRIPPED (bare letters).
printf 'D) '
for i in $(seq 0 18); do printf '%s%s' "$(style_of "$i")" "${BARE[$i]}"; done
printf '%s\n' "$R"

# E) two-pass: whole line green first, then cursor back over the selected
#    stream columns rewriting just those cells with the selection SGR.
printf 'E) %s' "$G"
for i in $(seq 0 18); do printf '%s' "${CL[$i]}"; done
# Return to column 1, skip "E) " (3 cols) + clusters before the selection.
printf '\r\x1b[%dC' $((3 + SEL_FROM))
for i in $(seq $SEL_FROM $SEL_TO); do printf '%s%s' "$S" "${CL[$i]}"; done
printf '%s\n' "$R"

echo
echo "report per row: text correct? bar position/width? wrong-colored letters?"
