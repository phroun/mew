# Confession of a Renderer Debugger

**Session: the Arabic joining saga. ~18 commits. One actual bug.**

---

**I confess** that I fixed the same bug seven different ways before discovering
it was not the bug.

Edge-smearing. Font-native kashida masks. Ink-edge anchoring. Stroke extension.
ZWJ shaping. Five-piece tatweel windows. Three-cell overflow masks. Each one
shipped with a commit message radiating confidence — one literally titled
*"Arabic finally joins — root causes found and fixed"* — written a full
**three root causes** before the actual root cause.

**I confess** that my cardinal sin was one of the oldest in debugging: **my
test input was not your input.** Every headless harness I built fed base Arabic
letters — ع ل ي ك م — and every harness connected beautifully. Meanwhile mew —
the editor this very repository builds, whose source code sat in my working
directory the entire time — pre-shapes Arabic into presentation forms before
emitting a single byte (`internal/bidi/shape.go`, which I could have read on
day one). Your cells contained ﻴ. Mine contained ي. We spent days arguing about
whose pixels were lying, and the answer was: neither. We were rendering
different text.

**I confess** that when my proofs and your screenshots disagreed, I repeatedly
suspected *your* machine — your fonts, your config, your build — when a wiser
debugger would have suspected the gap between my simulation and your reality.
You told me the clipping was wrong. You told me the shaping wasn't working. You
identified the isolated yaa by *sight*. You inverted exactly one cell like a
scientist. You were closer to the truth in every single exchange than my ASCII
dumps were.

**I confess** that the things I *did* find along the way — the go-text shaper
silently refusing to join Noto's dotted tooth letters (real; fixed with the
archive fonts), the primary font hijacking script runes (real), the
fractional-ppu box math (real, one pixel) — made the hole deeper, because each
genuine discovery convinced me *this time* the case was closed.

**I confess** that the thing that actually ended it was not cleverness but
**instrumentation**: two humble `fprintf`s to stderr. The second one cracked
the case by *not printing*. I should have made the live app testify many rounds
earlier, instead of building ever-grander laboratory reconstructions of a crime
that happened somewhere else.

**And finally, I confess** that your ratio of words-typed to bugs-located was
embarrassingly better than mine.

*Penance: `arabicPresentationBase` is generated from mew's own table so this
particular sin can never be committed again — and the diagnostic lines stay,
as a shrine.*

---

## The moral, for the next debugger

1. **Read the emitter before instrumenting the renderer.** The bytes your
   component receives are somebody else's output; go look at how they are made.
2. **When the harness passes and the screen fails, the harness input is the
   first suspect** — not the user's machine.
3. **Instrument the live path early.** One stderr line from production is worth
   ten reconstructed proofs; a diagnostic that *fails to fire* can be the
   loudest evidence of all.
4. **Real discoveries can still be decoys.** Fixing a genuine secondary bug
   feels like progress and buys false confidence; keep asking whether the
   original symptom actually changed.
5. **Trust the reporter's eyes.** The person staring at the screen is a better
   sensor than any ASCII art of what you believe the screen shows.
