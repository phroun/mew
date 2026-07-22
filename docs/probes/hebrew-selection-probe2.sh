#!/usr/bin/env bash
# Round 2: measure Terminal.app's attribute-boundary drift exactly.
#
# Round 1 established: per-glyph vs coalesced SGR and re-addressed writes
# render IDENTICALLY (A=B=E); LRO is not honored (C garbled with a visible
# replacement glyph); bare letters behave differently from pointed (D vs A).
# Working model: the terminal records style boundaries as CODEPOINT offsets
# and applies them as CELL offsets, so each boundary drifts logically-later
# by the number of combining marks before it.
#
# This round selects a SINGLE known cell per row (or splits fg/bg) so the
# drift function can be read off directly.
#
# RUN:   bash hebrew-selection-probe2.sh
#
# ANSWER KEY — the sentence's letters numbered LOGICALLY:
#   1=alef 2=nun 3=yud 4=(space) 5=resh 6=vav 7=tsadi 8=he 9=(space)
#   10=lamed 11=shin 12=tav 13=vav 14=tav 15=(space) 16=mem 17=yud
#   18=mem-sofit 19=(period)
#
# REPORT per row: WHICH NUMBERED LETTER(S) carry the white background
# (or the red foreground in G1). "None visible" is also an answer.

G=$'\x1b[0;32;40m'   # green on black
S=$'\x1b[0;30;47m'   # black on white (selection)
RED=$'\x1b[0;31;40m' # red on black (fg-only test)
R=$'\x1b[0m'

CL=("אֲ" "נִ" "י" " " "ר" "וֹ" "צֶ" "ה" " " "לִ" "שְׁ" "תּ" "וֹ" "ת" " " "מַ" "יִ" "ם" ".")
BARE=("א" "נ" "י" " " "ר" "ו" "צ" "ה" " " "ל" "ש" "ת" "ו" "ת" " " "מ" "י" "ם" ".")

# emit_one LABEL from to style-for-span [array=CL|BARE]
emit_span() {
  local label=$1 from=$2 to=$3 spanstyle=$4 arr=$5
  printf '%s) ' "$label"
  for i in $(seq 0 18); do
    local st=$G cl
    if [ "$i" -ge "$from" ] && [ "$i" -le "$to" ]; then st=$spanstyle; fi
    if [ "$arr" = BARE ]; then cl=${BARE[$i]}; else cl=${CL[$i]}; fi
    printf '%s%s' "$st" "$cl"
  done
  printf '%s\n' "$R"
}

echo
echo "== round 2: drift measurement =="
echo "report: which NUMBERED letter(s) are on white (G1: which are red)"
echo

# F0 control: BARE letters, single cell 12 (tav of lishtot) selected.
#     Expected everywhere: white on letter 12 exactly.
emit_span F0 11 11 "$S" BARE

# F1: POINTED, single cell 6 (vav-holam of rotzeh) selected.
#     Drift model predicts white lands 2 letters later (letter 8, he).
emit_span F1 5 5 "$S" CL

# F2: POINTED, single cell 12 (tav+dagesh of lishtot) selected.
#     Drift model predicts white lands 5 letters later (letter 17, yud).
emit_span F2 11 11 "$S" CL

# F3: POINTED, single cell 14 (final tav of lishtot) selected.
#     Drift model predicts the white falls PAST the text (nothing white,
#     or on the period).
emit_span F3 13 13 "$S" CL

# G1: POINTED, cells 6..14 with FOREGROUND change only (red, black bg).
#     Does the RED land on the correct letters (6-14) or drift like the bar?
emit_span G1 5 13 "$RED" CL

# G2: POINTED, cells 6..14 with the full selection style (bar reference).
emit_span G2 5 13 "$S" CL

# H) mid-cluster SGR legality: the whole sentence green, but a white-bg SGR
#    is injected BETWEEN the shin base and its shin-dot/sheva (letter 11),
#    reverting to green before the next base. The planned fix must place
#    background changes at codepoint positions, which fall mid-cluster.
#    Report: (1) does the shin still render correctly POINTED?
#            (2) which numbered letter (if any) shows the white background?
printf 'H) '
for i in $(seq 0 18); do
  if [ "$i" -eq 10 ]; then
    printf '%sש%sְׁ%s' "$G" "$S" "$G"
    continue
  fi
  printf '%s%s' "$G" "${CL[$i]}"
done
printf '%s\n' "$R"

echo
echo "answer key: 1=alef 2=nun 3=yud 4=sp 5=resh 6=vav 7=tsadi 8=he 9=sp"
echo "            10=lamed 11=shin 12=tav 13=vav 14=tav 15=sp 16=mem 17=yud 18=mem 19=."
