// Parity tests for the JS port — mirror ifitfits_test.go so JS and Go behave
// identically. Run: node ifitfits.test.js
"use strict";
const ff = require("./ifitfits.js");
const { Viewport, NewViewport, Up, Down, Left, Right, On, Off, Normal, Zoom, LTR, TTB } = ff;

let failures = 0;
function ok(cond, msg) { if (!cond) { console.log("FAIL: " + msg); failures++; } }
function near(a, b) { return Math.abs(a - b) < 0.6; }

// helpers reaching internals (the tree is plain objects, like the Go test does)
function selChild(n) { for (const c of n.children) if (c.selected) return c; return n.children[0]; }
function selectChild(g, c) { for (const x of g.children) x.selected = x === c; }
function leaf(v, ref, mn) { const l = v._newLeaf(ref); if (mn) { l.minW = l.minH = 20; l.natW = l.natH = mn; } return l; }
function grp(v, o, ...kids) { return v._newGroup(o, ...kids); }
// mirror Go's test setRoot: install a hand-built tree and re-parent it (the
// library rebuilds parents in _resolve, but tests poke ops before that runs).
function rebuildParents(r) { r.parent = null; (function w(n) { for (const c of n.children || []) { c.parent = n; w(c); } })(r); }
function setRoot(v, r) { v.root = r; rebuildParents(r); v._touch(); }
function rectOf(v, h) { v._ensure(); return v.handles.get(h).rect; }

// TestBasicLayout
(function () {
  const v = new Viewport(300, 100);
  const a = leaf(v, "a", 100), b = leaf(v, "b", 100), c = leaf(v, "c", 100);
  setRoot(v, grp(v, LTR, a, b, c));
  ok(near(rectOf(v, a.handle).X, 0) && near(rectOf(v, a.handle).W, 100), "basic a");
  ok(near(rectOf(v, b.handle).X, 100), "basic b");
  ok(near(rectOf(v, c.handle).X, 200), "basic c");
})();

// TestZoomClimbsToStackBox
(function () {
  const v = new Viewport(300, 100);
  const t1 = leaf(v, "t1", 100), t2 = leaf(v, "t2", 100);
  const s = grp(v, LTR, t1, t2); selectChild(s, t1);
  const q = leaf(v, "q", 100);
  setRoot(v, grp(v, LTR, s, q));
  v.Zoom(t1.handle, On); v._ensure();
  ok(s.mode === Zoom, "zoom climbed to stack box");
  ok(t1.mode !== Zoom, "zoom left the tab");
  ok(s.rect.W > q.rect.W, "zoom took effect");
})();

// TestEqualizeThenBalance
(function () {
  const v = new Viewport(360, 100);
  const a = leaf(v, "a"), b = leaf(v, "b"), c = leaf(v, "c");
  a.minW = 20; a.natW = 50; b.minW = 20; b.natW = 250; c.minW = 20; c.natW = 120;
  for (const l of [a, b, c]) { l.minH = 20; l.natH = 100; }
  setRoot(v, grp(v, LTR, a, b, c));
  v.Resize(a.handle, 200);
  v.Equalize(a.handle, false); v._ensure();
  ok(near(a.rect.W, 120) && near(b.rect.W, 120) && near(c.rect.W, 120), "equalize -> 120 each");
  v.Balance(a.handle, false); v._ensure();
  ok(!a.pin.has, "balance cleared pin");
  ok(near(a.rect.W, 50) && near(c.rect.W, 120), "balance -> naturals");
})();

// TestLensTargets
(function () {
  const v = new Viewport(400, 200);
  const a1 = leaf(v, "a1"), a2 = leaf(v, "a2"), colA = grp(v, TTB, a1, a2), b = leaf(v, "b");
  setRoot(v, grp(v, LTR, colA, b));
  v.Monocle(a1.handle, On);
  ok(v._lensTarget(v.lens.tile, v.lens.group) === a1 && v.lens.scope === "screen", "monocle=tile/screen");
  v.Monocle(a1.handle, Off);
  v.Spectacle(a1.handle, On);
  ok(v._lensTarget(v.lens.tile, v.lens.group) === colA && v.lens.scope === "screen", "spectacle=group/screen");
  v.LocalSpectacle(a1.handle, On);
  ok(v.lens.scope === "group", "local_spectacle fills group");
})();

// TestMonocleFillsScreenAndDismisses
(function () {
  const v = new Viewport(400, 200);
  const a = leaf(v, "a"), b = leaf(v, "b"); a.minW = a.minH = b.minW = b.minH = 20;
  setRoot(v, grp(v, LTR, a, b));
  v.Monocle(a.handle, On); v._ensure();
  ok(near(a.rect.W, 400) && near(a.rect.H, 200), "monocle fills screen");
  ok(b.hidden, "monocle hides b");
  ok(!b.navHidden, "b still navigable");
  v.SetFocus(b.handle);
  ok(v.lens === null, "lens dismisses on focus outside");
})();

// TestEdgeSlideFlip
(function () {
  const v = new Viewport(300, 100);
  const band = leaf(v, "band"); band.minW = band.minH = 20;
  setRoot(v, grp(v, LTR, band)); v._ensure();
  v.caretX = 0;
  const seq = []; for (let i = 0; i < 3; i++) { v.Go(band.handle, Right); seq.push(v.caretX); }
  ok(near(seq[0], 150) && near(seq[1], 300) && near(seq[2], 300), "edge-slide right 0->150->300->300");
  v.caretX = 300;
  const back = [150, 0, 0]; let good = true;
  for (const w of back) { v.Go(band.handle, Left); if (!near(v.caretX, w)) good = false; }
  ok(good, "edge-slide left 300->150->0->0");
})();

// TestEdgeSlideRoute
(function () {
  const v = new Viewport(300, 200);
  const band = leaf(v, "band"), p = leaf(v, "p"), q = leaf(v, "q");
  for (const l of [band, p, q]) l.minW = l.minH = 20;
  setRoot(v, grp(v, TTB, band, grp(v, LTR, p, q))); v._ensure();
  v.caretX = 150; v.caretY = 40;
  v.Go(band.handle, Right);
  ok(v.Go(band.handle, Down) === q.handle, "right-edge then down -> q");
  v.caretX = 150; v.caretY = 40;
  v.Go(band.handle, Left);
  ok(v.Go(band.handle, Down) === p.handle, "left-edge then down -> p");
})();

// TestNavBreaksOutOfMonocle
(function () {
  const v = new Viewport(400, 200);
  const band = leaf(v, "band"), p = leaf(v, "p"), q = leaf(v, "q");
  for (const l of [band, p, q]) l.minW = l.minH = 20;
  setRoot(v, grp(v, TTB, band, grp(v, LTR, p, q)));
  v.Monocle(band.handle, On); v._ensure();
  const dest = v.Go(band.handle, Down);
  ok(dest === p.handle || dest === q.handle, "nav breaks out of monocle");
})();

// TestRefCloneAndLookup
(function () {
  const [v, first] = NewViewport(400, 200);
  v.Set(first, "doc");
  const n2 = v.New(first, Right);
  ok(v.Content(n2) === "doc", "new clones ref");
  const n3 = v.New(first, Right, "other");
  ok(v.Content(n3) === "other", "new explicit ref");
  ok(v.Get("doc", true).length === 2, "Get(doc) -> 2");
  v.Set(n2, "doc2");
  ok(v.Get("doc", true).length === 1, "after Set -> 1");
})();

// TestCaretPerAxisRehome
(function () {
  const v = new Viewport(300, 200);
  const a = leaf(v, "a"), b = leaf(v, "b"), c = leaf(v, "c");
  for (const l of [a, b, c]) l.minW = l.minH = 20;
  setRoot(v, grp(v, TTB, a, b, c)); v._ensure();
  v.caretX = 37; v.caretY = b.rect.Y + b.rect.H / 2;
  v.Close(b.handle);
  ok(near(v.caretX, 37), "close keeps perpendicular caret.x");
})();

// TestTabOdometerWalkBack
(function () {
  const v = new Viewport(400, 200);
  const sb = grp(v, LTR, leaf(v, "s1"), leaf(v, "s2"), leaf(v, "s3")); selectChild(sb, sb.children[0]);
  const w18 = leaf(v, "w18"), split = grp(v, TTB, sb, w18), w8 = leaf(v, "w8");
  const o = grp(v, LTR, split, w8); selectChild(o, split);
  const focus = leaf(v, "F");
  setRoot(v, grp(v, LTR, o, focus));
  selectChild(o, w8); selectChild(sb, sb.children[2]);
  v.visCur = null; v._touch(); v._ensure();
  v.TabPrior(focus.handle); const first = selChild(sb).ref;
  v.TabPrior(focus.handle); const second = selChild(sb).ref;
  ok(first !== second, "odometer walk-back cycles the inner (" + first + "->" + second + ")");
})();

// TestTabCycleWrapsOutermost
(function () {
  const v = new Viewport(400, 200);
  const inner = grp(v, LTR, leaf(v, "i0"), leaf(v, "i1")); selectChild(inner, inner.children[1]);
  const outer = grp(v, LTR, leaf(v, "o0"), inner); selectChild(outer, inner);
  setRoot(v, grp(v, LTR, outer, leaf(v, "side")));
  const dest = v.TabNext(inner.children[1].handle);
  ok(v.handles.get(dest) && v.handles.get(dest).ref === "o0", "wrap lands on outermost o0");
})();

// TestSplitAddsTileAndSubdivides — guards the newGroup self-cycle regression
// (Split dropping the new tile; a follow-on directional New hanging the parent walk).
(function () {
  const [v, first] = NewViewport(720, 420);
  v.Set(first, "win1");
  const right = v.Split(first, Right);
  ok(v.Tiles().length === 2, "Split(Right) yields 2 tiles");
  ok(v.Content(right) === "win1", "Split clones origin ref");
  const down = v.New(right, Down); // used to hang on the self-cycle
  ok(v.Tiles().length === 3, "New(Down) after Split yields 3 tiles");
  ok(down !== 0 && v.Content(down) === "win1", "New clones origin ref");
})();

if (failures === 0) console.log("ALL PASS (13 parity groups)");
else { console.log(failures + " FAILURES"); process.exit(1); }
