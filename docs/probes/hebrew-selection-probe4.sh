#!/usr/bin/env bash
# Round 4: do per-glyph PEN ATTRIBUTES (bold, underline) ride the reordered
# glyphs, unlike background/reverse?
#
# Settled so far (Terminal.app, flipBidiForHost):
#   - foreground color RIDES the glyphs (round-2 G1: exact).
#   - background AND reverse-video are placed by a codepoint-vs-cell miscount
#     and drift when combining marks are present (round-3 I: span 6-14
#     inverted only 11-14).
# So the selection bar cannot come from bg or reverse. The remaining
# glyph-riding channels are pen attributes. If bold and/or underline ride,
# mew renders flip-mode selection as (syntax fg kept) + attribute on exactly
# the selected cells: correctly positioned and unambiguous.
#
# RUN:  bash hebrew-selection-probe4.sh
#
# REPORT per row: which NUMBERED letters carry the attribute (bold / underline)
# and whether the pointed text still reads correctly.

G=$'\x1b[0;32;40m'      # green on black (normal)
BOLD=$'\x1b[1;32;40m'   # bold green   (attribute test: does bold ride?)
UL=$'\x1b[0;4;32;40m'   # underline green
BU=$'\x1b[1;4;93;40m'   # PROPOSED flip-selection: bold+underline+bright-yellow fg
R=$'\x1b[0m'

CL=("אֲ" "נִ" "י" " " "ר" "וֹ" "צֶ" "ה" " " "לִ" "שְׁ" "תּ" "וֹ" "ת" " " "מַ" "יִ" "ם" ".")

emit_span() { # LABEL from to spanstyle
  local label=$1 from=$2 to=$3 spanstyle=$4
  printf '%s) ' "$label"
  for i in $(seq 0 18); do
    local st=$G
    if [ "$i" -ge "$from" ] && [ "$i" -le "$to" ]; then st=$spanstyle; fi
    printf '%s%s' "$st" "${CL[$i]}"
  done
  printf '%s\n' "$R"
}

echo
echo "== round 4: do bold / underline ride the glyphs? =="
echo "report: which NUMBERED letters carry the attribute; text still correct?"
echo

# L) BOLD over cells 6..14. If bold rides, letters 6-14 are bold.
emit_span L 5 13 "$BOLD"

# M) UNDERLINE over cells 6..14.
emit_span M 5 13 "$UL"

# N) the PROPOSED selection look over 6..14 (bold+underline+bright-yellow fg).
#    This is exactly what mew would emit for a selected cell in flip mode.
emit_span N 5 13 "$BU"

# O) single-cell bold: letter 6 alone (drift check).
emit_span O 5 5 "$BOLD"
# P) single-cell bold: letter 14 alone.
emit_span P 13 13 "$BOLD"

echo
echo "answer key: 1=alef 2=nun 3=yud 4=sp 5=resh 6=vav 7=tsadi 8=he 9=sp"
echo "            10=lamed 11=shin 12=tav 13=vav 14=tav 15=sp 16=mem 17=yud 18=mem 19=."
