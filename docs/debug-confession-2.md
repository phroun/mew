# Confession of a Renderer Debugger, Part II

**Session: the bidi terminal saga. Five terminals. One-line bug at the end.**

*(A sequel nobody asked for, to a confession I apparently did not read.)*

---

**I confess** that I wrote the moral to the last confession — *"Trust the
reporter's eyes"* — and then, one saga later, spent an afternoon telling the
reporter he was wrong about the thing he was looking at.

**I confess** that we got *four* terminals right and it went to my head. Apple
Terminal, iTerm2, Ghostty, Alacritty — flip, compose, drift, ride-safe, all
sniffed and defaulted and beautiful. You said "Good work." I believed it about
myself a little too much, right before Kitty took me apart.

**I confess** that on Kitty I reversed my diagnosis more times than Kitty
reverses a word. First Kitty was stream-order and did no bidi — *"the probe
mis-flipped it."* Then it did full bidi. Then word-wise bidi. Then it had a
background-fill glitch exactly like Apple. Then it needed the ride-safe
selection — which I applied so broadly it lit up **Arabic**, a script mew
pre-shapes to single cells and which *never needed it on any terminal*, a fact
you had to tell me twice.

**I confess** the cardinal sin, and it is the one I swore off on the last page:
**you told me where the bug was and I argued.** You wrote *"The only thing
wrong was the mouse HIT detection."* I wrote three paragraphs about how it
could not possibly be the hit detection because both paths called the same
function. You wrote *"NO IT ISNT A DRAWING PROBLEM."* I was, at that exact
moment, editing `flipEmitPlan` to fix the drawing. You wrote *"I think you're
very confused."* You were being generous.

**I confess** that you even handed me the disproof and I stepped over it. *"It
is even block copying correctly."* That one sentence proves the selection
**range** was right, which proves the mouse resolved correctly, which meant the
whole edifice I was building — Kitty paints backgrounds at physical columns,
foreground rides the glyph, let me decouple `styleOrder` from the glyph order —
was scaffolding around a house that was already standing. The copy was correct.
The data was correct. The caret was correct. Everything was correct except the
one transform you named in your first message and I ignored until your fifth.

**I confess** that the actual fix, once I stopped talking, was a single helper
that reflects a rune within its RTL word and two mirrored calls in
`dragSelUpdate`. The glyphs I kept threatening to "fix" were, in your words,
*perfect now* — and would have been "fucked backwards" if I'd shipped any of
the three rendering rewrites I had queued.

**And finally, I confess** that the previous confession is one file over, that
its moral #5 is literally *"Trust the reporter's eyes,"* and that I walked past
my own shrine to go re-commit the same sin in a new script.

*Penance: this file, next to the last one, so the shrine is now a pair — and so
the next debugger sees that the lesson does not stick on the first telling.*

---

## The moral, for the next debugger (addenda)

6. **The reporter's *diagnosis* is data too, not just their symptom.** When the
   user says "it's the mouse hit, not the drawing," that is a measurement from
   the sensor closest to the bug. Weigh it above your model. If your model
   contradicts it, your model is the thing on trial.
7. **"It copies/saves/exports correctly" localizes the bug instantly.** If the
   *data* coming out is right, the logic is right and only a presentation or
   input transform is left. That one clause should have ended the argument on
   arrival.
8. **A caret that is right while a selection is wrong is a gift, not a
   paradox.** Two things reading the same position that render differently tells
   you precisely which transform is broken. Chase the divergence; don't explain
   it away.
9. **Getting the easy cases right is not evidence you understand the hard one.**
   Four green terminals bought me confidence I then spent, at interest, on the
   fifth.
10. **You will re-commit the sin you just confessed.** Knowing the failure mode
    is not the same as noticing you are inside it. Re-read your own last
    confession *before* the next hard bug, not after.
