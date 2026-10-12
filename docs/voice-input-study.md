# Voice input — a study (not yet built, nothing decided)

A brainstorming session's findings, written down so they outlast it. **Nothing
here is a commitment.** Where something is a verified fact about the current
tree it says so; where it is an opinion it is written as one.

The motivating use case is operating mew with little or no typing — hands-free
or hands-limited — and doing it in a way that uses what a long-time mew,
WordStar or Joe user already knows, rather than asking them to learn a new
vocabulary at the worst possible moment.

---

## 0. Which project this is

This is **mew**, not KittyTK. Worth stating because the first pass of this study
got it wrong and aimed at the toolkit's machinery.

- **mew** (repo root, `github.com/phroun/mew`) — the editor. Its own input layer,
  its own key sequence processor, its own command vocabulary, its own config.
- **mew plain** (`go build ./app/cmd/mew`, the `!kittytk` build) — mew driving the
  terminal directly. The reference build for evaluating behaviour, never shipped.
- **flagship mew** (`-tags kittytk`) — the same editor plus a full KittyTK host.
- **KittyTK** — a separate project with a separate key system. Not this.

**mew plain is the right development surface for all of it.** A terminal app
reading its own input, with no display server, no wire and no toolkit, is the
simplest place to build and test a phrase or key grammar. The flagship build
inherits whatever lands there. Whether voice should *also* reach host-level
things — menus, windows, tabs — is a later and separate question for a
different project.

---

## 1. The premise

There is no editor that handles voice anywhere near as well as a WordStar-family
command set could, and none that leverages a user's learned WordStar knowledge
at all.

The state of the art is **Talon** — a general voice-control engine with a
constrained grammar, used seriously by people who cannot type, with community
support for Vim and Emacs. Vim does reasonably well by accident: "three delete
word" is speakable. Nothing addresses WordStar's layer.

**Why WordStar is the interesting case:** its command layer is already a spoken
language that got compressed into chords. `^K B` is *block begin*, `^Q F` is
*quick find*, `^O R` is *onscreen right margin*. The prefixes were chosen as
mnemonics — K for blocK, Q for Quick, O for Onscreen — so the chord abbreviates
a phrase that is already in the user's head. Voice decompresses it.

**But split WordStar in half before porting it.** The `^K`/`^Q`/`^O` command
layer is word-shaped. The diamond (`^E`/`^S`/`^D`/`^X`) is *finger*-shaped — a
spatial optimisation for a hand on home row — and saying "ess ess ess" to move
three characters is miserable. Take the command layer; reach for motion through
search instead, which is what `^Q F` always was.

---

## 2. What mew already has (verified against the tree)

This is the part that changes the shape of the work. Most of what a voice layer
would want already exists for other reasons.

**The command layer is PawScript.** A key is not bound to a function; it is
bound to a **command string**, and `executeCommand` runs that string through
PawScript (`internal/editor/dispatch.go`). There is no command system to invent.

**There are 172 registered commands, and their names are already English.**
`block_copy`, `block_move`, `set_block_begin`, `go_line_beg`, `go_word_prior`,
`del_word_end`, `find_next`, `go_mark`. A spoken-command layer is close to an
identity transform over the existing vocabulary. That is not luck: the names are
WordStar's menu words, and those were English before they were chords.

**mew is already WordStar, in plain config files.**
`internal/editor/resources/defaults/keys_block_menu.conf`:

```
^K B	=set_block_begin
^K K	=set_block_end
^K C	=block_copy
^K M	=block_move
^K Y	=block_delete
```

and `keys_quick_menu.conf` carries `^Q F =find`, `^Q B =go_block_begin`,
`^Q L =go_line`, `^Q 1..0 =go_mark '1'`. **"Leverage learned WordStar knowledge"
is not a feature to design — it is the existing binding layer.** A voice layer is
a second spelling of these same files against the same command strings.

**The config system already has what a voice map needs.** `[mappings:mew]` is a
*named* set with `@include` and user override, and class-scoped sections
(`[isearch::mappings]`) refine every set — which is exactly how a grammar would
narrow inside incremental search, a prompt or a terminal viewport, where the
spoken vocabulary must change.

**The help system is already indexed by command name.** The WordStar menus *are*
the help topics (`resources/help/keys_block.txt`, `keys_quick.txt`), written as
`[[keys#block_copy|^K C]]` — anchor is the command, label is the key. And
`Editor.KeyForCommand` already answers "which key do I advertise for this". A
spoken form has an obvious seat beside the key form in a thing already on screen.

**And mew already has a spoken vocabulary for keys**, written for a different
reason. From `internal/editor/keydefaults.go`:

```go
// Word spellings for punctuation, so a binding can name a key the syntax
// it is written in would otherwise fight over. `-` is the modifier
// separator, so `M--` reads badly; `M-minus` says it plainly.
{"-", "minus"}, {"+", "plus"}, {"=", "equals"}, {"'", "apos"},
{"~", "tilde", "wave"}, {"`", "backtick"}, {"/", "slash"}, {"|", "pipe"},
```

plus `esc`/`escape`, `return`/`enter`, `back`/`backspace`, `del`, `fdel`, `tab`.
Pronounceable words standing for keys, with several spellings collapsing to one
token — and the collapse is already first-class, as `keyseq.FallbackGroup`. The
written binding syntax needed words where symbols fought with it. **Voice has the
same problem for the same reason, and the answer is already in the tree.**

**The one real gap: nothing can talk to a running mew from outside.**
`executeCommand` is reached from keys, the mouse, committed IME text, and
launch-time `--eval`. There is no socket, no listener, no live command channel.
`--eval` proves the execution side already works the way voice needs — scripts
run in the live session, exactly like a keybound command, and may prompt, sleep
or suspend. What is missing is only the *arrival*.

---

## 3. Two layers, and which is the floor

### 3a. Spoken keys — the floor

Saying the chord: "control K B", "control K K", "control Q F", "function
return". A **meta-language about typing**, not about editing. mew already half
speaks it: `^Q F` is pronounced "control Q F" by everyone who has ever taught it.

Three reasons this is the floor rather than a curiosity:

- **It never goes stale.** A phrase map needs an entry per command — 172 and
  counting, maintained in parallel with the keymap forever, and silent about
  anything a user rebinds. A key grammar is perhaps a hundred tokens and covers
  *every* binding that exists or ever will, including the user's own and
  including `not_implemented "Justify Block"`. Complete by construction.
- **It costs nothing downstream.** If spoken keys arrive as key tokens they
  enter at `dispatchKey`, and the sequence processor, capture levels, wildcards,
  the prefix help that appears after `^K`, QuickHelp and `repeat_next` are all
  untouched. Say "control K", watch the block menu appear, *then* say "C".
- **It cannot be mistaken for prose.** "control K B" is obviously not dictation.
  "block copy" is a sentence someone might write.

**The hard part is letters.** B/D/E/P/T/V are the classic recognition collision —
why aviation spells. "Control K Bravo" is far more robust than "Control K B", and
`FallbackGroup` is already the right shape for it: several spoken tokens
resolving to one key token. Expect to accept both, with the alphabet as the
reliable form. This is the load-bearing piece of Talon and the part it got right.

### 3b. Spoken commands — the convenience layer

"block copy" → `block_copy`. A config file, nothing more, given §2. Optional,
covers the dozen things done constantly, and the floor still answers when a
phrase does not exist. **Build the floor first and this becomes a config file.**

They are not rivals, and the order matters: the floor makes the convenience layer
cheap, where the reverse is not true.

---

## 4. Out of scope, deliberately

- **Do not ship a recognizer.** Command recognition needs a closed vocabulary
  handed to the engine in advance. Whisper is dictation-shaped — latency, no
  constrained grammar. Something external listens; mew receives.
- **Prose dictation is a different problem** wearing the same word. Let the OS or
  the recognizer do it. Solve commanding first.
- **KittyTK's own voice story**, if it ever has one, is its own project's
  question (see §0).

---

## 5. Open questions

1. **What is the channel, and who may use it?** The only genuinely new mechanism.
   A socket invites more than voice — scripting, remote control, a second process
   driving a session — and that is either a feature or a security problem
   depending on intent. This choice shapes everything downstream.
2. **What does it carry?** Key tokens is the lower, more complete door (§3a).
   Command strings is the higher one. Probably both, and probably text too, since
   a prompt (`find`) needs characters, not keys.
3. **How is dictation told from commanding**, if mew ever accepts both — a
   lead-in word, a mode, or a decision left entirely to the recognizer.
4. **Does any of this want to be in `mew.go`'s public API**, so an embedding host
   can drive a session the same way? Related to 1.

---

## 6. First milestone, whenever it comes

Phrases and keys as **text**, delivered down the channel, asserted headlessly the
way everything else in this tree is tested. No microphone in the loop. That
proves the grammar, the scoping and the mode handling — the parts that are
actually hard — before any recognizer exists, and it keeps the hard parts under
the same test discipline as the rest of the editor rather than being debugged
through a microphone.
