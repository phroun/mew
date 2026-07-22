# Embedded font provenance (the 12 files the sync patch requires)

All files are copied into `text/fonts/` after applying `kittytk-sync.patch`
(the patch carries `fonts.go`, whose `go:embed` directives fail the build until
the files exist). **The authoritative byte-exact source for every file is this
repository: `mew/kittytk/text/fonts/`.** The table below adds external origin
URLs — all on `raw.githubusercontent.com` — with verification status.

Licenses: all Noto families are SIL OFL 1.1 (see `text/fonts/OFL.txt`);
Source Han Serif is SIL OFL 1.1 (Adobe).

## Byte-verified external sources (sha256 matches our files exactly)

These four Arabic files are the shaping-critical ones — they MUST be these
exact archive ("phase-2 hinted") builds. Current Noto Arabic releases use
chained-contextual GSUB that go-text/typesetting cannot execute, leaving
medial letters isolated; `text/arabicjoin_test.go` (TestEmbeddedArabicFacesJoin)
fails on any non-joining substitute.

| File | sha256 | Source URL |
|---|---|---|
| NotoNaskhArabic-Regular.ttf | `4ab181a31934ea8a7827aeb76f2d96b15f261fc8caceb1a0ef14c380aa54dc2b` | https://raw.githubusercontent.com/googlefonts/noto-fonts/main/archive/hinted/NotoNaskhArabic/NotoNaskhArabic-Regular.ttf |
| NotoNaskhArabic-Bold.ttf | `8af2de1b73345397a12716368c90cb8470a4fe15d5f49b52ec5e58ecd20f4da1` | https://raw.githubusercontent.com/googlefonts/noto-fonts/main/archive/hinted/NotoNaskhArabic/NotoNaskhArabic-Bold.ttf |
| NotoKufiArabic-Regular.ttf | `fd1be306f86805b278f1368cfada717b1cc39eb3a96e407c3641cf8b6d4f8bb2` | https://raw.githubusercontent.com/googlefonts/noto-fonts/main/archive/hinted/NotoKufiArabic/NotoKufiArabic-Regular.ttf |
| NotoKufiArabic-Bold.ttf | `559cc97e760ecb1984924b7fe17a36135d06300cd465ab8d30d0c0e3aaadefae` | https://raw.githubusercontent.com/googlefonts/noto-fonts/main/archive/hinted/NotoKufiArabic/NotoKufiArabic-Bold.ttf |
| NotoSansCJKsc-Regular.otf | `2c76254f6fc379fddfce0a7e84fb5385bb135d3e399294f6eeb6680d0365b74b` | https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf (also matches `main` today) |

(The `archive/` tree in googlefonts/noto-fonts is frozen; `main` is a stable
ref for it. None of these repos use Git-LFS for these paths — a fetched file
of ~130 bytes means a wrong URL, not an LFS pointer.)

## Version-identified; byte-exact only from this repo

These are cosmetic/coverage faces (no shaping dependency); a same-version
rebuild from the canonical projects would also function, but the exact bytes
we ship could not be located on a reachable host — they are `GOOG`-vendor
static instances produced by the fonts.google.com download pipeline (the
notofonts.github.io per-project builds are different, larger binaries of the
same versions). Copy them from `mew/kittytk/text/fonts/`.

| File | Exact version (name table) | sha256 (ours) | Canonical project |
|---|---|---|---|
| NotoSerif-Regular.ttf | Version 2.015 (GOOG static) | `a7e8ea1a…7bbe939d` | github.com/notofonts/latin-greek-cyrillic (via fonts.google.com "Noto Serif") |
| NotoSerif-Bold.ttf | Version 2.015 (GOOG static) | `1b941681…48f0fe0a` | 〃 |
| NotoSerif-Italic.ttf | Version 2.015 (GOOG static) | `15d5f805…03741e43` | 〃 |
| NotoSerif-BoldItalic.ttf | Version 2.015 (GOOG static) | `24003396…24b899c6` | 〃 |
| NotoSerifHebrew-Regular.ttf | Version 2.004 (GOOG static) | `5e72a179…20c3fe02` | github.com/notofonts/hebrew (via fonts.google.com "Noto Serif Hebrew") |
| NotoSerifHebrew-Bold.ttf | Version 2.004 (GOOG static) | `ff3e3495…be6389cc` | 〃 |
| NotoSerifCJKsc-Regular.otf | Version 2.003 (Adobe build, self-reports "Source Han Serif SC") | `ff80afff…4b5bbb67` | **github.com/adobe-fonts/source-han-serif @ tag `2.003R` (commit 7cedb7f), file `OTF/SimplifiedChinese/SourceHanSerifSC-Regular.otf`** — see the CRITICAL note below |

### CRITICAL: the serif CJK file — which build passes, which fails

The test `text/scriptclass_test.go` asserts the *family name* the serif CJK
face self-reports (`"Source Han Serif SC"`), NOT its sha256. So the requirement
is an **Adobe-built** Source Han Serif SC, whatever the exact bytes:

- ✅ **USE:** `github.com/adobe-fonts/source-han-serif` @ tag `2.003R`,
  `OTF/SimplifiedChinese/SourceHanSerifSC-Regular.otf`
  (sha256 `78aa7a32…464117`). This is NOT byte-identical to our embedded
  `ff80afff…` file, but it self-reports `"Source Han Serif SC"` — **verified:
  it passes the full `text` suite in a freshly-patched tree.** Fetch it via
  raw.githubusercontent.com (reachable) at the pinned tag, or copy the
  byte-exact file from `mew/kittytk/text/fonts/`.
- ❌ **DO NOT USE:** anything from `notofonts/noto-cjk` (the `Serif/OTF/...`
  tree). Those are Google-branded builds that self-report `"Noto Serif CJK
  SC"`, so `scriptclass_test.go` fails — this is the exact mismatch a
  re-sourcing attempt hits. (The *Sans* CJK is the opposite: it IS the
  Noto-named build from `notofonts/noto-cjk`, verified matching, so only the
  serif needs the Adobe name.)

Full 64-hex sha256 for the second table:

    a7e8ea1ab7e7d368b001c892d6148325fffc849a46e030e55e1bdc597bbe939d  NotoSerif-Regular.ttf
    1b941681106aae26ead3a24c6f15b51f994b1b26adce8742a37ce96648f0fe0a  NotoSerif-Bold.ttf
    15d5f8050fc1f142545e7a26a50bd07a0434eca535d4d98aad9d5e8f03741e43  NotoSerif-Italic.ttf
    2400339667ad2701320aa671e337d1632731e56b397564afe14e981124b899c6  NotoSerif-BoldItalic.ttf
    5e72a179024ad0463437b73b948f6d282da7d5b301a66e7f31efa05220c3fe02  NotoSerifHebrew-Regular.ttf
    ff3e349594c618b58d0044c25d6aa4045bba312f07ee6627197f4285be6389cc  NotoSerifHebrew-Bold.ttf
    ff80afff645eff9777f61f5969589eda3a439cb0e4bd0d0760c7b5124b5bbb67  NotoSerifCJKsc-Regular.otf
