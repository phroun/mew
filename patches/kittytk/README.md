# KittyTK upstream sync patch

**STATUS: LANDED upstream in KittyTK `v0.1.3-alpha`** (commits `c6854ac` "Apply
downstream mew sync patch: cursive Arabic, script-aware fonts, TUI cipher" and
`d835c71` "Use genuine Adobe Source Han Serif SC…", build bumped to 3). mew's
vendored `./kittytk` is now re-synced to that release: fonts, `fonts.go`, and
`core/version.go` adopted from the tag, so the only remaining vendored↔upstream
divergence is the mew boundary below. `app/go.mod` pins
`github.com/phroun/kittytk v0.1.3-alpha`. This directory is kept as the
development record.

`kittytk-sync.patch` brought upstream KittyTK (`github.com/phroun/kittytk`,
developed against main @ `27e64de`) up to date with the improvements developed
in mew's vendored fork (`mew/kittytk`), minus everything that properly belongs
to mew itself. Verified: applied clean to `27e64de`, and the patched tree built
and passed its FULL test suite standalone (`GOWORK=off go build ./... &&
go test ./...`) with no mew module anywhere in the graph.

Note on the release fonts: upstream re-sourced the embedded fonts from
`FONTS.md` rather than copying mew's exact bytes, so the release's Arabic/Serif
builds differ byte-for-byte from the ones originally verified on-screen — but
they are valid joining builds (`TestEmbeddedArabicFacesJoin` passes on the
release tree) and mew has adopted them for consistency. The genuine Adobe
Source Han Serif SC (`78aa7a32…`, adobe-fonts/source-han-serif @ tag `2.003R`)
replaces mew's earlier byte-twin.

## What it achieves

53 files, +4664/−246, plus 12 embedded font binaries (see "Applying"). The
headline areas:

1. **Cursive Arabic in the PurfecTerm gfx renderer** — the centerpiece. Cells
   holding Arabic (base letters or the presentation forms bidi-aware apps
   emit) are joined for real: each cell shapes a window of prev + tatweels +
   letter + tatweels + next as ONE run so the font's GSUB produces true
   contextual forms, then keeps an exactly-cell-wide slice centred on the
   letter whose cut ends land mid-stroke, so adjacent cells meet at their
   boundaries. Includes the presentation-form→base reverse map (standard
   Unicode Forms-A/B data), the Unicode joining-type classifier, and one-time
   stderr diagnostics (`arabic face=… join=…` / `arabic geom=…`) that report
   the resolved face, join verdict, and slice geometry from a live run.
2. **Embedded font set that actually shapes** — Noto Naskh + Kufi Arabic
   swapped to the archive (phase-2 hinted) builds: current Noto Arabic
   releases implement the dotted "tooth" letters via chained-contextual GSUB
   that go-text/typesetting does not execute, so runs shaped with them leave
   the middle letters ISOLATED (`TestEmbeddedArabicFacesJoin` locks the
   requirement to the embedded faces). Also adds Noto Serif (4 styles), Noto
   Serif Hebrew, and Sans/Serif CJK SC.
3. **Systematic script-aware font tree** — `ui-{text,term}-{western,hebrew,
   arabic,cjk}-{sans,serif}` aliases with per-glyph script-class resolution;
   script-classed runes resolve to their script face BEFORE the primary, so a
   Latin-centric primary with incidental coverage of a few script codepoints
   can neither render wrong isolated forms nor split the shaping run.
   `RuneSpanX` cluster-span queries on shaped paragraphs. Font loading from
   files/dirs (`fontload.go`), `SetFontAlias` chains, engine epochs.
4. **PurfecTerm renderer parity with the wire protocol** — per-cell font slots
   (SGR 10–20 / OSC 7004), script-class fonts (OSC 7005), VTFRAKTUR (SGR 20),
   mouse reporting/visual fixes, exact cell-rect mask sizing at fractional
   pixels-per-unit (mask box now uses the same edge math as cell-rect fills).
5. **TUI cipher pseudo-fonts** — Unicode Mathematical-Alphanumeric ciphering
   for bold/italic/fraktur on plain terminals, with `[tui]` hostcfg gating and
   an independent `fraktur_mode`.
6. **Host config + fixes** — `[fonts]`/`fonts_path`/`ui_*` alias overrides in
   kittytk.ini wired into both hosts; SGR 20 in style; editor-trinket host
   seams (`SetLaunchArgv`, `SetShowDesktop/HideDesktop`) on the placeholder;
   window manager/tearoff fixes; macOS About-menu hook (`sdl/aboutmenu_*`);
   `examples/editordemo`; Makefile fix (upstream's `-tags mew` build cannot
   compile upstream, where the mew editor file does not exist).

Regression tests ride along at the layer that earned them: the Arabic render
tests feed BOTH base letters and presentation forms and assert identical
masks, and an end-to-end test paints through the real raster backend and
asserts the joined baseline on the actual framebuffer.

## Applying

From the upstream kittytk checkout at `27e64de`:

    git apply kittytk-sync.patch
    cp <mew>/kittytk/text/fonts/{NotoKufiArabic-*.ttf,NotoNaskhArabic-*.ttf,\
NotoSerif-*.ttf,NotoSerifHebrew-*.ttf,NotoSansCJKsc-Regular.otf,\
NotoSerifCJKsc-Regular.otf} text/fonts/
    GOWORK=off go build ./... && GOWORK=off go test ./...

The 12 font binaries (~43 MB) are shipped as a copy step rather than inflating
this patch; their bytes live in `mew/kittytk/text/fonts/`. Per-file provenance
— exact versions, sha256 hashes, and verified external source URLs (all on
raw.githubusercontent.com) — is in `FONTS.md`. For a single self-contained
artifact instead, run in the patched tree:
`git add -A && git diff --staged --binary > full.patch`.

Note: the patch's `go.mod`/`go.sum` carry NO `github.com/phroun/mew`
requirement (the vendored tree needs it only for the build-tagged
`editor_mew.go`, which this patch excludes; the upstream module graph stays
mew-free — verified zero references after apply).

## Deliberately excluded (the mew boundary)

- `objects/trinkets/editor_mew.go`, `editor_protocol_mew.go` — the mew-backed
  editor (`//go:build mew`, imports `github.com/phroun/mew`). Upstream keeps
  the placeholder; the patch extends the placeholder's contract surface only.
- `core/version.go` — upstream's Build counter (already ahead); bump on merge.
- `README.md` — upstream's license/support additions are newer; untouched.
- `garland/` — actively developed upstream; untouched (mew consumes the
  separate `github.com/phroun/garland` module instead).

After applying, the ONLY divergence between upstream and mew's vendored tree
is that list — verified by full-tree diff.

## Judgment calls to review

- The one-time Arabic stderr diagnostics are unconditional (one line per
  process, only when Arabic renders). They earned their keep; gate them if
  upstream prefers silence.
- Comments name mew as the reference consumer (matching upstream's existing
  editor-contract voice) but no longer reference mew internals.
