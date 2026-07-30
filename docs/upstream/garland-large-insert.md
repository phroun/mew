# Bug report: a single large `InsertString`/`InsertBytes` builds one oversized leaf

**From:** mew (editor integrating garland v0.1.10)
**Concerns:** `tree.go` — `insertIntoLeaf`; contrast with `garland.go` — `buildBalancedSubtree`
**Type:** Performance / structural defect, with a proposed fix and a caller-side workaround

> ## ✅ Fixed in garland v0.1.11
>
> Resolved upstream the same day. `tree.go` gains `buildEditSubtree`, an
> edit-time counterpart to `buildBalancedSubtree` that stamps the current
> fork/revision, passes `-1` for the leaf file offset, splits on rune
> boundaries, and partitions decorations across the leaves it builds;
> `insertIntoLeaf` calls it when `len(data) > maxLeafSize`. Upstream added
> `large_insert_test.go`, which asserts the structural invariant directly
> (no leaf over `maxLeafSize`) rather than timing it, and covers insert into
> the middle, decoration partitioning, undo/redo, large overwrite, and UTF-8
> boundaries.
>
> Re-running the repro below against v0.1.11, every case is **1×** the
> loader baseline — see "After the fix" at the end. mew is on v0.1.11 and
> carries a regression guard at
> `internal/buffer/insert_structure_test.go`.
>
> The rest of this document is kept as filed, describing v0.1.10.

## Summary

Content that arrives through **one large `InsertString`/`InsertBytes` call** is
stored as a **single leaf node holding the entire payload**, however large it
is. The loader path (`Open` with `DataString`/`DataBytes`/a file) splits the
same content into a balanced tree of leaves bounded by `maxLeafSize`.

The result is a document that is permanently slow to read — not slow to build.
A 3 MB insert leaves a buffer whose random line access is **63× slower** than
the identical content opened through the loader, and the ratio **grows with
file size**. It is not a warm-up cost: the measurements below are taken after
warming, and the penalty never goes away for the life of the document.

In mew this is the difference between "Insert File" (`^B R`) and "Open File"
(`^B O`) on the same file: same bytes, same editor, one of them leaves the
buffer sluggish forever.

## Reproduction

`insert_perf_test.go`, attached — a standalone module depending on nothing but
`github.com/phroun/garland v0.1.10`:

```sh
mkdir garlandrepro && cd garlandrepro
go mod init garlandrepro && go get github.com/phroun/garland@v0.1.10
# drop in insert_perf_test.go
go test -v -timeout 30m
```

## Measurements

Go 1.25.0, linux/amd64, Intel Xeon @ 2.10 GHz, 4 cores. Corpus is 50,000 lines
/ 3,077,780 bytes of plain ASCII. Each case builds the document a different
way, then runs the **same** workload against it: 500 scattered
`SeekLine`+`ReadLine` calls, warmed first.

| Case | How the content got there | 500 `SeekLine`+`ReadLine` |
|---|---|---|
| **A** | `Open(FileOptions{DataString: content})` | **78 ms** *(baseline)* |
| **B** | empty document + one `InsertString(content)` | **4.98 s** — 64× |
| **C** | 2-line document + one `InsertString(content)` | **5.00 s** — 64× |
| **D** | `Open`, then 1000 scattered single-rune inserts | **81 ms** — 1.0× |
| **E** | empty + same content in 4 KiB chunks, line-aligned | **140 ms** — 1.8× |

What each case rules in or out:

- **C ≈ B** — it is not about the document being empty. Inserting into
  populated content is just as bad.
- **D ≈ A** — it is not that mutation degrades the tree. Ordinary
  typing-sized edits leave the structure healthy, which is exactly what the
  coalescing path in `insertIntoLeaf` is there to do.
- **E ≈ A** — it is not the total byte volume, nor the call site, nor the
  number of calls. The *same* bytes through the *same* API in 4 KiB pieces
  are fine. **The trigger is the size of an individual insert.**

### It scales the wrong way

200 `SeekLine`+`ReadLine`, same two build paths, three corpus sizes:

| Lines | Opened | One big `InsertString` | Ratio |
|---|---|---|---|
| 25,000 | 34 ms | 965 ms | **29×** |
| 50,000 | 27 ms | 1.69 s | **63×** |
| 100,000 | 33 ms | 4.08 s | **123×** |

The opened path is flat, as a tree should be. The inserted path roughly
doubles its penalty each time the content doubles — the signature of a linear
scan over the whole payload per access.

### Both address spaces degrade

500 seeks, no reads, 50,000 lines:

| | `SeekByte` | `SeekLine` |
|---|---|---|
| Opened | 11.7 ms | 37.7 ms |
| One big `InsertString` | 440 ms (38×) | 2.37 s (63×) |

Byte and line addressing both degrade, so this is not one index in particular
— it is consistent with the leaf itself being oversized, with line addressing
paying the most.

## Mechanism (garland v0.1.10 source)

`Garland.insertIntoLeaf` (`tree.go:596`) has a coalescing fast path:

```go
// tree.go:643
combinedLen := int64(len(leftData)) + int64(len(data)) + int64(len(rightData))
if combinedLen <= 2*g.maxLeafSize {
        // ... build one leaf, or two balanced leaves. Healthy.
}
```

Past that threshold it falls through to the left/middle/right branch, and the
middle leaf takes the **entire** inserted payload with no size bound
(`tree.go:716-722`):

```go
// Create new middle leaf (inserted content)
middleSnap := createLeafSnapshot(data, absoluteDecs, -1)
```

With `DefaultMaxLeafSize = 128 KB` (`garland.go:52`), a 3 MB insert produces a
single leaf ~24× over the maximum, and a 12 MB insert one ~96× over. Every
subsequent seek that lands in that leaf scans it.

Compare the loader, which handles exactly this case (`garland.go:2919`):

```go
if dataLen <= g.maxLeafSize {
        // Small file - single leaf
} else {
        // Large file - build balanced tree
        contentNodeID, contentSnap = g.buildBalancedSubtree(data, 0)
}
```

So garland already knows how to turn a large byte slice into a well-formed
subtree. The insert path just doesn't call it. The invariant "no leaf exceeds
`maxLeafSize`" holds on load and holds under incremental editing, but a single
large insert violates it silently and permanently.

## Suggested fix

In `insertIntoLeaf`, when `int64(len(data)) > g.maxLeafSize`, build the middle
as a balanced subtree instead of one leaf, then concatenate as it does today.

`buildBalancedSubtree` is not directly reusable as written — it hardcodes
`setSnapshot(0, 0, …)` and threads a `fileOffset` for warm-storage mapping.
An edit-time sibling would stamp `g.currentFork, g.currentRevision` and pass
`-1` as the leaf file offset (matching `createLeafSnapshot(data, …, -1)` in the
current middle-leaf code), splitting on rune boundaries as
`buildBalancedSubtree` already does via `alignToRuneBoundary`.

Decorations need the same partitioning by the split points that the two-leaf
branch at `tree.go:669` already performs, generalized to N leaves.

We have not attempted the patch ourselves, since node lifecycle, snapshot
versioning and the decoration cache are areas where your invariants are much
better understood on your side than ours. If you would like us to take a run at
it, we're glad to.

## Caller-side workaround

Case **E** is available to integrators today: chunk large inserts into
`maxLeafSize`-ish pieces (4 KiB in the measurements, aligned to line
boundaries) and issue them as successive `InsertString` calls at the advancing
cursor. Each chunk then goes through the coalescing path and the tree stays
well-formed — within 1.8× of the loader.

That is a workaround, not a fix: it costs more calls, and it means every
integrator has to know a threshold that belongs to garland's internals. We'd
rather the library get this right.

## Not implicated

- **Undo coalescing.** `SetUndoCoalescing(true, …)` on vs off: 35.7 s vs
  37.4 s on the 200k-line corpus. Not a factor.
- **Build time.** Building via insert costs about 2× the loader (114 ms vs
  57 ms for 12 MB) — unremarkable, and not what the report is about. The cost
  is entirely in what the document becomes.
- **Cursor mode.** All measurements use `CursorModeHuman`.

## After the fix (garland v0.1.11)

The identical repro, same machine, no changes to the test file:

| Case | v0.1.10 | v0.1.11 |
|---|---|---|
| **A** opened | 78 ms | 77 ms |
| **B** empty + one big `InsertString` | 4.98 s *(64×)* | **80 ms** *(1.0×)* |
| **C** seeded + one big `InsertString` | 5.00 s *(64×)* | **80 ms** *(1.1×)* |
| **D** opened + 1000 single-rune inserts | 81 ms | 79 ms |
| **E** empty + 4 KiB-chunked | 140 ms | 126 ms |

Scaling is flat where it used to compound: 1× at 25k, 50k and 100k lines,
against 29× / 63× / 123× before. Byte and line seeks both return to baseline
(10.0 ms / 39.9 ms inserted, vs 10.9 ms / 37.9 ms opened).

At mew's own `Buffer` level — the real `^B R` path, `New()` + one
`Caret.Insert` of a 200,000-line file — scattered `GetLine` went from **242×**
the `^B O` path to **1.06×**, with content round-tripping identically through
both.

Note that the **chunking workaround is now counterproductive**: case E is
slower than case B on v0.1.11 (126 ms vs 80 ms), because many small inserts
build more nodes than one balanced subtree does. We never shipped it, and
integrators who did should drop it on upgrading.
