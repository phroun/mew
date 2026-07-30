#!/usr/bin/env python3
"""Generate a dense torture-sample of the character classes that broke mew's
renderer. Every invisible character is written as an explicit codepoint so
this generator stays readable."""

import io

U = chr  # codepoint -> character

# C0 controls (mew already classified these) plus DEL.
C0 = [U(0x01), U(0x02), U(0x07), U(0x0b), U(0x0e), U(0x1b), U(0x1c), U(0x1f), U(0x7f)]
# C1 controls — the range mew was blind to. 0x90/0x98/0x9b/0x9c/0x9d/0x9e/0x9f
# are STRING INTRODUCERS: a terminal swallows the rest of the line after one.
C1 = [U(0x80), U(0x85), U(0x90), U(0x94), U(0x98), U(0x9b), U(0x9c), U(0x9d), U(0x9f)]
# Marks from scripts mew cannot render — defective on ANY base.
NKO = [U(c) for c in (0x07eb, 0x07ec, 0x07ed, 0x07ee, 0x07f0, 0x07f1, 0x07f3)]
OTHER_MARKS = [U(c) for c in (0x0e31, 0x09bc, 0x0f71, 0x102e, 0x0951, 0x17b6, 0x1ab0, 0xa806)]
# Hebrew marks: well-formed on a Hebrew base, defective on anything else.
HEB_MARKS = [U(c) for c in (0x0591, 0x0597, 0x05b0, 0x05b4, 0x05c1)]

LOREM = [
    "Lorem ipsum dolor sit amet, consectetur adipiscing elit",
    "sed do eiusmod tempor incididunt ut labore et dolore magna",
    "Ut enim ad minim veniam, quis nostrud exercitation ullamco",
    "laboris nisi ut aliquip ex ea commodo consequat duis aute",
    "irure dolor in reprehenderit in voluptate velit esse cillum",
    "dolore eu fugiat nulla pariatur excepteur sint occaecat",
    "cupidatat non proident, sunt in culpa qui officia deserunt",
    "mollit anim id est laborum sed ut perspiciatis unde omnis",
    "iste natus error sit voluptatem accusantium doloremque",
    "laudantium, totam rem aperiam eaque ipsa quae ab illo",
    "inventore veritatis et quasi architecto beatae vitae dicta",
    "sunt explicabo nemo enim ipsam voluptatem quia voluptas sit",
    "aspernatur aut odit aut fugit, sed quia consequuntur magni",
    "dolores eos qui ratione voluptatem sequi nesciunt neque",
    "porro quisquam est, qui dolorem ipsum quia dolor sit amet",
    "consectetur, adipisci velit, sed quia non numquam eius modi",
    "tempora incidunt ut labore et dolore magnam aliquam quaerat",
    "voluptatem ut enim ad minima veniam, quis nostrum ipsa quae",
    "exercitationem ullam corporis suscipit laboriosam nisi aut",
    "aliquid ex ea commodi consequatur quis autem vel eum iure",
]


def strew(line, i):
    """Attach offenders to spread-out words of a lorem line."""
    pool = [
        C0[i % len(C0)],
        C1[i % len(C1)],
        NKO[i % len(NKO)],
        C1[(i + 3) % len(C1)],
        OTHER_MARKS[i % len(OTHER_MARKS)],
        C0[(i + 4) % len(C0)],
        HEB_MARKS[i % len(HEB_MARKS)],   # lands on a Latin letter: mixed script
        C1[(i + 6) % len(C1)],
        NKO[(i + 2) % len(NKO)],
    ]
    words = line.split(' ')
    step = max(1, len(words) // len(pool))
    k = 0
    for wi in range(len(words)):
        if wi % step == 0 and k < len(pool):
            words[wi] = words[wi] + pool[k]
            k += 1
    return ' '.join(words)


L = []
L.append("=== mew renderer torture sample =========================")
L.append("Dense mix of the classes that broke rendering: C0 and C1")
L.append("controls, marks from scripts mew cannot render, marks on a")
L.append("base of the wrong script, marks with no base, invalid UTF-8.")
L.append("No row may overrun its window, lose text, or drift the caret.")
L.append("")
L.append("--- dense mixed corruption ------------------------------")
for i, base in enumerate(LOREM):
    L.append(strew(base, i))

L.append("")
L.append("--- marks with NO base (they open the line) -------------")
L.append(U(0x0301) + "acute opens this line with nothing to anchor onto")
L.append(U(0x05b0) + "niqqud opens this one")
L.append(U(0x07ed) + "an NKo tone opens this one")
L.append(U(0x0301) * 3 + "three stacked marks, still no base at all")

L.append("")
L.append("--- C1 string introducers (each of these ate its line) --")
for name, cp in (("DCS", 0x90), ("SOS", 0x98), ("CSI", 0x9b),
                 ("ST ", 0x9c), ("OSC", 0x9d), ("PM ", 0x9e), ("APC", 0x9f)):
    L.append(name + " " + U(cp) + " the rest of this line must still be here")

L.append("")
L.append("--- wide glyphs interleaved with offenders --------------")
L.append("CJK " + U(0x65e5) + U(0x672c) + U(0x8a9e) + " then " + U(0x07ed) +
         " more " + U(0x65e5) + U(0x672c) + " and " + U(0x94) + " tail text")
L.append("emoji " + U(0x1f642) + " and " + U(0x1f680) + U(0x07ee) + " trailing text")
L.append(U(0x65e5) + U(0x0597) + " hebrew accent on a CJK base, then plain text")
L.append("tabs\tand\tcontrols\t" + U(0x1b) + " mixed\t" + U(0x9b) + " with wide " + U(0x4e2d))

L.append("")
L.append("--- zero-width and bidi controls ------------------------")
L.append("zwsp" + U(0x200b) + "here zwj" + U(0x200d) + "join bom" + U(0xfeff) + "mark")
L.append("rlm" + U(0x200f) + " rlo" + U(0x202e) + " fsi" + U(0x2066) + " here" + U(0x2069) + " end")

L.append("")
L.append("--- WELL-FORMED: these must still render perfectly ------")
L.append("pointed hebrew: " + U(0x05e9) + U(0x05c1) + U(0x05b8) + U(0x05dc) +
         U(0x05d5) + U(0x05b9) + U(0x05dd) + "  " + U(0x05e2) + U(0x05b8) +
         U(0x05dc) + U(0x05b5) + U(0x05d9) + U(0x05db) + U(0x05b6) + U(0x05dd))
L.append("arabic: " + U(0x0627) + U(0x0644) + U(0x0633) + U(0x064e) + U(0x0644) +
         U(0x0627) + U(0x0645) + "  " + U(0x0639) + U(0x064e) + U(0x0644) +
         U(0x064a) + U(0x0643) + U(0x064f) + U(0x0645))
L.append("latin diacritics: re" + U(0x0301) + "sume" + U(0x0301) +
         " nai" + U(0x0308) + "ve co" + U(0x0308) + "operate")
L.append("cyrillic: " + U(0x041f) + U(0x0440) + U(0x0438) + U(0x0432) + U(0x0435) +
         U(0x0301) + U(0x0442) + "   greek: " + U(0x03ba) + U(0x03b1) + U(0x03bb) +
         U(0x03b7) + U(0x03bc) + U(0x03ad) + U(0x03c1) + U(0x03b1))
L.append("cjk: " + U(0x65e5) + U(0x672c) + U(0x8a9e) + U(0x3068) +
         U(0x4e2d) + U(0x6587) + U(0x3068) + U(0xd55c) + U(0xad6d) + U(0xc5b4))

blob = '\n'.join(L).encode('utf-8')

# Raw invalid UTF-8, spliced in as bytes so the decoder yields U+FFFD.
bad = bytes([0x8f, 0xd2, 0x29, 0xbe, 0xfe, 0xff, 0x96, 0xb3, 0xae])
tail = ("\n--- invalid UTF-8 bytes ---------------------------------\n".encode()
        + b"quis nostrud" + bad[:3] + b" exercitation" + bad[3:6]
        + b" ullamco laboris" + bad[6:] + b" nisi ut aliquip ex ea\n"
        + b"duis aute\x8f irure\xd2 dolor\xfe in\xff reprehenderit\x96 velit esse\n"
        + b"excepteur\xb3 sint\xae occaecat\xfe cupidatat non proident sunt in\n"
        + b"culpa\xc2 qui\xe0 officia\xf0 deserunt mollit anim id est laborum\n")

out = blob + b'\n' + tail
io.open('lorem-torture.txt', 'wb').write(out)
print("wrote lorem-torture.txt:", len(out), "bytes,", out.count(b'\n'), "lines")
