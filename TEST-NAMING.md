# Test file naming policy

**Every test file is named for the source file it tests.**

```
0_<source-base>[_<differentiator>]_test.go     one source file is the subject
00_<description>_test.go                       no single source file is the subject
```

- **`0_`** — this file tests the behavior of exactly one source file. The base
  part is that file's name, spelled exactly as the file is spelled.
- **`00_`** — this file spans several source files, tests an end-to-end path, or
  holds shared helpers rather than tests. It carries a description instead.
- **`<differentiator>`** — present only when one source file has more than one
  test file, and only as long as it needs to be to tell them apart. A source
  with a single test file gets no differentiator at all.
- **`<description>`** on a `00_` file says what the test covers, not which files
  it happens to touch: `00_pointer_hover_test.go`, not
  `00_button_and_dock_hover_test.go`.

Tests sort to the top of a directory listing, cross-cutting ones first;
`ls 0_buffer*` answers "what tests `buffer.go`?" and its absence answers
"nothing does"; and a source file with no test beside it is a gap you can see
without looking for it.

`kittytk/TEST-NAMING.md` is the long form of this rule — the reasoning, the
mechanical attribution pass, and the shape it takes in other languages. It is
the same rule; mew adds the two sections below.

---

## Deciding the subject

**The test's subject is the file that would have to change if the test started
failing for a real reason.** Everything else the test touches is scaffolding,
and scaffolding dominates: a test that opens a buffer, installs a viewport and
presses a key names the buffer's file far more often than the file it is really
about. Counting mentions gets it backwards.

Reach for `00_` when the honest answer is that there is no single subject — a
path exercised end to end, one behavior verified across two collaborating
files, or a file of helpers with no tests of its own. Do not reach for it
because attribution was work.

---

## Traps in this repository

**A trailing `GOOS` or `GOARCH` word silently drops the file from the build.**
Go compiles `..._windows_test.go` only on Windows and `..._arm64_test.go` only
on arm64 — no warning, no error, the tests simply never run. mew is full of
names that want that word: a test about window sizing, a test about the Linux
clipboard path. Keep the platform word out of the trailing position —
`0_platform_windows_frame_test.go` is fine, `0_platform_frame_windows_test.go`
is not.

**A leading underscore is worse.** Go ignores any file whose name begins with
`_`. The `0_` prefix exists so that nobody needs a sort trick.

**mew's own build tags are not in the file name.** `sdl`, `webgpu`, `kittytk`,
`unix` and the rest are declared in `//go:build` lines, and a test file's name
says nothing about which of them select it. The one exception is the fork
boundary below, where the name is doing real work.

---

## The fork boundary

`kittytk/` is a git subtree of [phroun/kittytk](https://github.com/phroun/kittytk),
and mew's own files inside it are dropped from every upstream split **by name**.
The rule there is that a mew-owned Go file spells `mew` as an
underscore-delimited word — see [`docs/kittytk-subtree.md`](docs/kittytk-subtree.md)
and `kittytk/docs/fork-sync-policy.md`.

The two rules compose without an exception, because the subject of a mew test
inside the subtree is a mew-owned source file and therefore already carries the
word: `editor_mew_scrollbar.go` gives `0_editor_mew_scrollbar_test.go`. A `00_`
file has to be named for it deliberately — `00_mew_login_shell_test.go`, not
`00_login_shell_test.go`.

What does NOT survive is matching on a prefix. `editor_mew*` was the boundary
glob until this policy arrived, and it misses `0_editor_mew_blink_test.go`
entirely. Anything that identifies mew's files by name must split on `_` and
look for the word.

`kittytk/objects/trinkets/00_mew_fork_boundary_test.go` asserts that the name
and the `//go:build mew` tag agree on every Go file in the subtree, in both
directions, so neither half of this can drift unnoticed:

    (cd kittytk && go test -tags mew -run ForkBoundary ./objects/trinkets/)

---

## Renaming an existing test file

Every package follows this now, so the usual case is one file changing subject
rather than a sweep. The care below is the same either way: **renaming a test
file can drop it from the build without failing anything**, so prove it did not.

For a sweep, convert a package at a time, smallest first — the small ones settle
how much differentiator is enough and which collaborations are really `00_`
before you reach `internal/editor`, which holds more than half the test files in
the tree.

Snapshot the inventory first, under every configuration that selects different
files:

```sh
go test -list '.*' ./...                 | grep -E '^(Test|Benchmark|Fuzz|Example)' | sort > before.txt
go test -tags sdl -list '.*' ./...       | grep -E '^(Test|Benchmark|Fuzz|Example)' | sort > before-sdl.txt
go test -tags mew -list '.*' ./...       | grep -E '^(Test|Benchmark|Fuzz|Example)' | sort > before-mew.txt
```

`-list` includes benchmarks, fuzz targets and examples, and `./...` does not
reach a nested module — `app/` and `kittytk/` each need their own run.

Rename with `git mv`, commit each package separately, and say in the message
which renames changed the *subject* rather than only adding the prefix. Then
compare as sets, not as counts:

```sh
diff before.txt after.txt && echo "identical"
```

Equal counts prove nothing: a name that hides one file and a rename that
reveals another cancel out. Finish with the direct checks:

```sh
# Trailing GOOS/GOARCH word before _test.go
find . -name '*_test.go' -not -path './patches/*' | grep -E '_(aix|android|darwin|dragonfly|freebsd|hurd|illumos|ios|js|linux|netbsd|openbsd|plan9|solaris|wasip1|windows|zos|386|amd64|arm|arm64|loong64|mips|mips64|mips64le|mipsle|ppc64|ppc64le|riscv64|s390x|wasm)_test\.go$'

# Anything still unprefixed
find . -name '*_test.go' -not -name '0_*' -not -name '00_*' -not -path './patches/*'
```

Both print nothing. `patches/` is excluded because it holds other projects'
sources for patch authoring — `patches/purfecterm/_src` is purfecterm's tree,
named by purfecterm's rules and ignored by Go anyway for its `_` directory.
