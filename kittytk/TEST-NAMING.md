# Test file naming policy

**Every test file is named for the source file it tests.** Read the rule, then
the method for applying it. It is not specific to this project — it is meant to
be dropped into any repository large enough that "where are the tests for this
file?" has stopped having an obvious answer.

---

## The rule

```
0_<source-base>[_<differentiator>]_test.<ext>     one source file is the subject
00_<description>_test.<ext>                       no single source file is the subject
```

- **`0_`** — this file tests the behavior of exactly one source file. The base
  part is that file's name, spelled exactly as the file is spelled.
- **`00_`** — this file spans several source files, tests an end-to-end path, or
  holds shared helpers rather than tests. It carries a description instead of a
  source name.
- **`<differentiator>`** — present only when one source file has more than one
  test file, and only as long as it needs to be to tell them apart. `keymap.go`
  with four test files gets `0_keymap_test`, `0_keymap_case_test`,
  `0_keymap_serial_test`, `0_keymap_terminal_test`. A source with one test file
  gets no differentiator at all.
- **`<description>`** on a `00_` file is succinct and says what the test covers,
  not which files it happens to touch: `00_pointer_hover_test`, not
  `00_button_and_dock_hover_test`.

### What this buys

- **Tests sort to the top of the directory listing, together, above the
  sources.** `00_` sorts before `0_`, so the cross-cutting tests lead and the
  per-file ones follow.
- **`ls 0_foo*` answers "what tests foo.go?"** without a search, and its absence
  answers "nothing does."
- **A source file with no `0_` file is visible** as a gap, at a glance, in a
  listing you were already looking at.
- **A test file whose subject does not exist is visible too.** That is not
  hypothetical: it is how a file gets orphaned when its subject moves or is
  removed, and the name is the only thing that will tell you.

### What it does not mean

Many `0_bigfile_*` files are not a naming problem. They are a reading of how
much behavior one source file owns, which is worth knowing and is not the
policy's business to hide.

---

## Deciding the subject

The old file name is a claim about the subject, not evidence of it. Verify it.

**The test's subject is the file that would have to change if the test started
failing for a real reason.** Everything else the test touches is scaffolding.

That distinction matters more than it sounds, because scaffolding dominates.
A test that builds a whole window, puts a widget in it and clicks the widget
mentions the window's file far more often than the widget's. Counting mentions
gets it backwards.

### The mechanical pass

Index every symbol each source file defines, then score each test file by the
symbols it uses that only one source defines. Run this from a package
directory (the example is Go; the shape is the same anywhere):

```python
import re, glob, collections
srcs = [f for f in glob.glob("*.go") if not f.endswith("_test.go")]
defs = collections.defaultdict(set)
pat = re.compile(r'^(?:func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)'
                 r'|type\s+([A-Za-z_]\w*)|(?:var|const)\s+([A-Za-z_]\w*))', re.M)
for s in srcs:
    for m in pat.finditer(open(s).read()):
        n = m.group(1) or m.group(2) or m.group(3)
        if n: defs[n].add(s)
for tf in sorted(glob.glob("*_test.go")):
    body = open(tf).read()
    cnt = collections.Counter()
    for i in set(re.findall(r'\b[A-Za-z_]\w*\b', body)):
        if i in defs and len(defs[i]) == 1:
            cnt[next(iter(defs[i]))] += body.count(i)
    print(f"{tf:44s} {cnt.most_common(3)}")
```

This is a hint generator, not a decision procedure. Treat a clear majority for
one file as a strong signal, and treat anything else as a question.

### The judgment pass

For every file the mechanical pass did not settle — and to sanity-check the
ones it did — read the test file's leading comment and the names of its first
two or three tests. In a well-commented repository this is usually decisive in
one line, and it is the only step that catches a test whose old name pointed at
the wrong thing.

Reach for `00_` when the honest answer is that there isn't one subject: a path
exercised end to end, one behavior verified across two collaborating files, or
a file of helpers with no tests of its own. Do not reach for it because
attribution was work.

---

## Traps

**A trailing platform or architecture word can silently exclude the file from
the build.** In Go, a file ending `_windows_test.go` or `_arm64_test.go` is
compiled only on that platform — no warning, no error, the tests simply never
run. It is easy to hit by accident: a test about window resizing wants to be
called `..._windows_test.go`. Check every new name against the full list of
`GOOS` and `GOARCH` values before committing, and only as a *trailing* word —
`0_platform_windows_frame_test.go` is fine, `0_platform_frame_windows_test.go`
is not. Other toolchains have their own version of this; find out what yours
ignores before you invent a suffix.

**A leading underscore is worse.** Go ignores any file whose name starts with
`_` entirely. Never use a bare `_` prefix as a sort trick — that is exactly what
the `0_` prefix is for.

**A helper file with no tests still needs a name.** It takes `00_`, because it
serves several test files and is the subject of none.

**A file whose subject is not in this repository takes the subject's name
anyway.** If `0_widget_extra_test` names a `widget_extra` that no longer
exists, that is the file telling you it has been orphaned. Naming it something
vague hides the fact.

---

## Adopting this in an existing repository

Renaming test files can drop tests from the build without failing anything.
Prove it did not.

**1. Snapshot the inventory first**, under every build configuration the
project uses:

```sh
go test -list '.*' ./...            | grep -E '^(Test|Benchmark|Fuzz|Example)' | sort > before.txt
go test -tags sdl -list '.*' ./...  | grep -E '^(Test|Benchmark|Fuzz|Example)' | sort > before-sdl.txt
```

Note that `-list` includes benchmarks, fuzz targets and examples, and that a
nested module is not reached by `./...` at all.

**2. Rename with `git mv`**, so history follows the file.

**3. Go package by package, smallest first.** The small ones settle the
vocabulary — how much differentiator is enough, which collaborations are really
`00_` — before you reach the package with a hundred files in it. Commit each
group separately, and say in the message which renames changed the *subject*
rather than just adding the prefix. Those are the ones a reviewer needs to see.

**4. Compare the inventory after each group**, as sets and not as counts:

```sh
diff before.txt after.txt && echo "identical"
```

Identical sets mean nothing was silently excluded. Equal counts do not: a name
that hides one file and a rename that reveals another cancel out.

**5. Check for accidental exclusions directly**, as a belt to the braces:

```sh
# Trailing GOOS/GOARCH word before _test.go
find . -name '*_test.go' | grep -E '_(aix|android|darwin|dragonfly|freebsd|hurd|illumos|ios|js|linux|netbsd|openbsd|plan9|solaris|wasip1|windows|zos|386|amd64|arm|arm64|loong64|mips|mips64|mips64le|mipsle|ppc64|ppc64le|riscv64|s390x|wasm)_test\.go$'

# Anything still unprefixed
find . -name '*_test.go' -not -name '0_*' -not -name '00_*'
```

Both should print nothing.

---

## Other languages

The three parts — marker, subject, differentiator — carry over; only their
position changes to suit what the test runner discovers.

| Language | Form |
|---|---|
| Go | `0_parser_errors_test.go` |
| Python (pytest) | `0_parser_errors_test.py`, or `test_0_parser_errors.py` where the runner insists on the leading `test_` |
| JS/TS (jest, vitest) | `0_parser.errors.test.ts` |
| Rust | applies to `tests/`; unit tests in a `mod tests` block are already adjacent to their subject and need nothing |

Check what your runner globs for before choosing, and check what your toolchain
silently ignores.
