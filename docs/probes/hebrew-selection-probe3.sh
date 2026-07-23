#!/usr/bin/env bash
# Round 3: does REVERSE VIDEO ride the glyphs like foreground does?
#
# Round 2 established: fg color rides the reordered glyphs perfectly (G1:
# red exactly on letters 6-14, pointed); the BACKGROUND plane is placed by
# a baroque codepoint-column mapping (F1 at parse columns; F3 apparently
# wrapping; F2 vanishing) not worth reverse-engineering.
#
# If the reverse-video ATTRIBUTE rides the glyph like fg does, mew can
# render flip-mode selections as fg=white + reverse — which displays
# black-on-white without ever placing a background span. These rows prove
# or kill that.
#
# RUN:  bash hebrew-selection-probe3.sh
#
# REPORT per row: which NUMBERED letters appear black-on-white
# (same answer key as round 2), and whether the text reads correctly.

G=$'\x1b[0;32;40m'    # green on black
V=$'\x1b[0;7;37;40m'  # REVERSE + white fg on black bg -> shows black on white
R=$'\x1b[0m'

CL=("אֲ" "נִ" "י" " " "ר" "וֹ" "צֶ" "ה" " " "לִ" "שְׁ" "תּ" "וֹ" "ת" " " "מַ" "יִ" "ם" ".")

emit_span() { # LABEL from to
  local label=$1 from=$2 to=$3
  printf '%s) ' "$label"
  for i in $(seq 0 18); do
    local st=$G
    if [ "$i" -ge "$from" ] && [ "$i" -le "$to" ]; then st=$V; fi
    printf '%s%s' "$st" "${CL[$i]}"
  done
  printf '%s\n' "$R"
}

echo
echo "== round 3: reverse-video ride test =="
echo "report: which NUMBERED letters are black-on-white; text correct?"
echo

# I) reverse span over cells 6..14 (the round-2 G2 span). If reverse rides
#    the glyphs, the inverted region sits EXACTLY on letters 6-14.
emit_span I 5 13

# J) reverse on a single cell: letter 6 (vav-holam) alone.
emit_span J 5 5

# K) reverse on a single cell: letter 14 (final tav) alone.
emit_span K 13 13

echo
echo "answer key: 1=alef 2=nun 3=yud 4=sp 5=resh 6=vav 7=tsadi 8=he 9=sp"
echo "            10=lamed 11=shin 12=tav 13=vav 14=tav 15=sp 16=mem 17=yud 18=mem 19=."
