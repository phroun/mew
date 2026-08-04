// ifitfits — a renderer-agnostic viewport tiling engine.
//
// This JavaScript library is a line-for-line port of the Go package
// github.com/phroun/ifitfits, exposing the SAME API (method names, argument
// order, and return values) so a host calls it identically in either language:
//
//   const [vp, first] = ifitfits.NewViewport(1920, 1080);
//   const right = vp.Split(first, ifitfits.Right);   // clones first's ref
//   for (const b of vp.Tiles()) draw(b.Ref, b.Rect);
//   const dest = vp.Go(first, ifitfits.Right);        // returns the destination tile
//
// The engine owns the layout, not focus: every command takes an explicit tile
// handle; the host reports focus (SetFocus) only so a lens knows when to dismiss.
// See COMMANDS.md for the mew (PawScript) viewport_* names each method mirrors.
(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.ifitfits = api;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  // ---- enums (match the Go const blocks) ----
  const Up = 0, Down = 1, Left = 2, Right = 3, Before = 4, After = 5, Prior = 6, Next = 7;
  const Normal = 0, Zoom = 1, Shrink = 2;
  const Toggle = 0, On = 1, Off = 2;
  const LTR = 0, RTL = 1, TTB = 2, BTT = 3;

  const defMinW = 90, defMinH = 64, defNatW = 240, defNatH = 150, headerH = 24;

  // ---- free helpers ----
  function isH(o) { return o === LTR || o === RTL; }
  function axisOf(o) { return isH(o) ? "H" : "V"; }
  function defOrient(ax) { return ax === "H" ? LTR : TTB; }

  function walk(n, f) {
    if (!n) return;
    f(n);
    for (const c of n.children) walk(c, f);
  }
  function stacked(n) {
    if (n.kind !== "group") return false;
    for (const c of n.children) if (c.selected) return true;
    return false;
  }
  function selectedChild(n) {
    for (const c of n.children) if (c.selected) return c;
    return n.children.length ? n.children[0] : null;
  }
  function selectChild(g, child) { for (const c of g.children) c.selected = c === child; }
  function axOf(n) {
    if (n.kind === "leaf") return "";
    if (stacked(n)) return "Z";
    return axisOf(n.orient);
  }
  function firstLeaf(n) { let r = null; walk(n, x => { if (!r && x.kind === "leaf") r = x; }); return r; }
  function lastLeaf(n) { let r = null; walk(n, x => { if (x.kind === "leaf") r = x; }); return r; }
  function leavesOf(n) { const a = []; walk(n, x => { if (x.kind === "leaf") a.push(x); }); return a; }
  function rebuildParents(rt) { rt.parent = null; walk(rt, n => { for (const c of n.children) c.parent = n; }); }
  function indexOf(g, c) { return g.children.indexOf(c); }
  function indexOfNode(s, n) { return s.indexOf(n); }
  function reverse(s) { s.reverse(); }
  function removeNode(s, n) { const i = s.indexOf(n); if (i >= 0) s.splice(i, 1); return s; }
  function isAncestorOf(a, b) { for (let n = b; n; n = n.parent) if (n === a) return true; return false; }
  function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }

  function enclosingStack(n) { for (let p = n && n.parent; p; p = p.parent) if (stacked(p)) return p; return null; }

  function side(o, d) {
    if (d === Before || d === After) return d;
    switch (o) {
      case LTR: return d === Left ? Before : After;
      case RTL: return d === Left ? After : Before;
      case TTB: return d === Up ? Before : After;
      case BTT: return d === Up ? After : Before;
    }
    return After;
  }
  function axisFor(d) { return d === Up || d === Down ? "V" : "H"; }
  function contraryOrient(g) {
    if (!g.parent) return g.orient;
    return axisOf(g.parent.orient) === "H" ? defOrient("V") : defOrient("H");
  }

  // ---- mode / pin state machine ----
  function groupHasZoom(g) { if (!g) return false; for (const c of g.children) if (c.mode === Zoom) return true; return false; }
  function enterZoom(n) {
    if (n.parent) for (const c of n.parent.children) if (c.pin.has && c.pin.enforced) c.pin.enforced = false;
    n.mode = Zoom;
  }
  function restoreInactive(g) { for (const c of g.children) if (c.pin.has && !c.pin.enforced && c.mode === Normal) c.pin.enforced = true; }
  function setMode(n, m) {
    if (m === Zoom) { enterZoom(n); return; }
    const wasZoom = n.mode === Zoom;
    n.mode = m;
    if (m === Shrink && n.pin.has) n.pin.enforced = false;
    if (m === Normal && n.pin.has && !groupHasZoom(n.parent)) n.pin.enforced = true;
    if (wasZoom && n.parent && !groupHasZoom(n.parent)) restoreInactive(n.parent);
  }
  function cycleModeVal(cur, dir) { return (cur + dir + 3) % 3; }
  function clearPin(n) { n.pin = { amount: 0, has: false, enforced: false }; }
  function climbTiling(n) {
    while (n && n.parent && (stacked(n.parent) || n.parent.children.length === 1)) n = n.parent;
    return n;
  }
  function focusEntry(n) {
    while (n && n.kind === "group") {
      if (n.children.length === 0) return null;
      n = stacked(n) ? selectedChild(n) : n.children[0];
    }
    return n;
  }
  function nextFocusAfterDismiss(t) {
    for (let cur = t; cur.parent; cur = cur.parent) {
      const kids = cur.parent.children, i = kids.indexOf(cur);
      if (i + 1 < kids.length) return firstLeaf(kids[i + 1]);
      if (i - 1 >= 0) return lastLeaf(kids[i - 1]);
    }
    return null;
  }
  function swapNodes(a, b) {
    const pa = a.parent, pb = b.parent, ia = pa.children.indexOf(a), ib = pb.children.indexOf(b);
    pa.children[ia] = b; pb.children[ib] = a; a.parent = pb; b.parent = pa;
  }
  function opTargetOf(n) { if (!n) return null; return n.parent && stacked(n.parent) ? n.parent : n; }
  function baseGroup(t) { return t.parent ? t.parent : t; }
  function destStack(D, src) { let st = null; for (let n = D; n; n = n.parent) if (stacked(n) && !isAncestorOf(n, src)) st = n; return st; }
  function balanceGroup(g) { if (g.kind !== "group" || stacked(g)) return; for (const c of g.children) c.pin = { amount: 0, has: false, enforced: false }; }

  const FLIP = { [LTR]: RTL, [RTL]: LTR, [TTB]: BTT, [BTT]: TTB };

  // ---- layout ----
  function measure(n) {
    if (n.kind === "leaf") { n.mMinW = n.minW; n.mMinH = n.minH; n.mNatW = n.natW; n.mNatH = n.natH; return; }
    for (const c of n.children) measure(c);
    const sum = f => n.children.reduce((a, c) => a + f(c), 0);
    const max = f => n.children.reduce((m, c, i) => (i === 0 || f(c) > m ? f(c) : m), 0);
    if (stacked(n)) {
      const res = headerH;
      n.mMinW = max(c => c.mMinW); n.mNatW = max(c => c.mNatW);
      n.mMinH = max(c => c.mMinH) + res; n.mNatH = max(c => c.mNatH) + res;
    } else if (isH(n.orient)) {
      n.mMinW = sum(c => c.mMinW); n.mNatW = sum(c => c.mNatW); n.mMinH = max(c => c.mMinH); n.mNatH = max(c => c.mNatH);
    } else {
      n.mMinH = sum(c => c.mMinH); n.mNatH = sum(c => c.mNatH); n.mMinW = max(c => c.mMinW); n.mNatW = max(c => c.mNatW);
    }
  }
  function computeEff(n) {
    if (n.kind === "leaf") { n.eff = n.priority; return n.eff; }
    let m = 0;
    n.children.forEach((c, i) => { const e = computeEff(c); if (i === 0 || e > m) m = e; });
    n.eff = m; return m;
  }
  function markHidden(n, reason) { n.hidden = true; n.why = reason; for (const c of n.children) markHidden(c, reason); }
  function waterfill(items, pool, capFn) {
    let active = items.filter(k => capFn(k) - k.main > 0.5), g = 0;
    while (pool > 0.5 && active.length && g++ < 500) {
      const share = pool / active.length; let prog = false;
      for (const k of active) { const give = Math.min(share, capFn(k) - k.main); if (give > 0) { k.main += give; pool -= give; prog = true; } }
      active = active.filter(k => capFn(k) - k.main > 0.5);
      if (!prog) break;
    }
    return pool;
  }
  function allocate(n, x, y, w, h) {
    n.hidden = false; n.rect = { X: x, Y: y, W: w, H: h }; n.why = "";
    if (n.kind === "leaf") return;
    if (stacked(n)) {
      const res = n.headerH = headerH, child = selectedChild(n);
      for (const c of n.children) if (c !== child) markHidden(c, "tab");
      if (child) allocate(child, x, y + res, w, Math.max(0, h - res));
      return;
    }
    const horiz = isH(n.orient), mainAvail = horiz ? w : h, crossAvail = horiz ? h : w;
    const kids = n.children;
    for (const k of kids) k.omit = false;
    for (const k of kids) { const cmin = horiz ? k.mMinH : k.mMinW; if (cmin > crossAvail + 0.5) k.omit = true; }
    const active = kids.filter(k => !k.omit);
    for (const k of active) { k.mainMin = horiz ? k.mMinW : k.mMinH; k.mainNat = horiz ? k.mNatW : k.mNatH; k.main = 0; }
    const idx = k => kids.indexOf(k);
    const byPri = active.slice().sort((a, b) => (b.eff - a.eff) || (idx(a) - idx(b)));
    let used = 0;
    for (const k of byPri) { if (used + k.mainMin <= mainAvail + 0.5) { k.main = k.mainMin; used += k.mainMin; } else k.omit = true; }
    let surplus = Math.max(0, mainAvail - used);
    let vis = active.filter(k => !k.omit);
    for (const k of byPri) {
      if (k.omit || !(k.pin.has && k.pin.enforced)) continue;
      const want = Math.max(k.mainMin, k.pin.amount), give = Math.min(want - k.main, surplus);
      if (give > 0) { k.main += give; surplus -= give; }
    }
    const zoom = vis.filter(k => k.mode === Zoom);
    if (zoom.length && surplus > 0.5) { const share = surplus / zoom.length; for (const k of zoom) k.main += share; surplus = 0; }
    if (surplus > 0.5) {
      const normals = vis.filter(k => k.mode === Normal && !(k.pin.has && k.pin.enforced));
      surplus = waterfill(normals, surplus, k => Math.max(k.mainNat, k.main));
    }
    if (surplus > 0.5) {
      let cand = vis.filter(k => k.mode !== Shrink && !(k.pin.has && k.pin.enforced));
      if (!cand.length) cand = vis.slice();
      cand.sort((a, b) => idx(a) - idx(b));
      const last = cand[cand.length - 1]; if (last) last.main += surplus;
      surplus = 0;
    }
    const ordered = vis.slice().sort((a, b) => idx(a) - idx(b));
    let cur = 0;
    for (const k of ordered) {
      let cx, cy, cw, ch;
      if (horiz) { cw = k.main; ch = crossAvail; cy = y; cx = n.orient === LTR ? x + cur : x + (w - cur - cw); }
      else { ch = k.main; cw = crossAvail; cx = x; cy = n.orient === TTB ? y + cur : y + (h - cur - ch); }
      cur += k.main; allocate(k, cx, cy, cw, ch);
    }
    for (const k of kids) if (k.omit) markHidden(k, "omit");
  }
  function normalize(n) {
    if (n.kind !== "group") return;
    for (const c of n.children) normalize(c);
    let changed = true, guard = 0;
    while (changed && guard++ < 2000) {
      changed = false;
      for (let i = n.children.length - 1; i >= 0; i--) { const c = n.children[i]; if (c.kind === "group" && c.children.length === 0) { n.children.splice(i, 1); changed = true; } }
      for (let i = 0; i < n.children.length; i++) { const c = n.children[i]; if (c.kind === "group" && !stacked(c) && !stacked(n) && c.orient === n.orient) { n.children.splice(i, 1, ...c.children); changed = true; i--; } }
      for (let i = 0; i < n.children.length; i++) { const c = n.children[i]; if (c.kind === "group" && c.children.length === 1) { const only = c.children[0]; only.selected = c.selected; n.children[i] = only; changed = true; } }
    }
  }

  // ---- odometer helpers (loose tab cycle) ----
  function visStacksOf(root) { const a = []; walk(root, n => { if (n.kind === "group" && stacked(n) && !n.hidden && n.children.length >= 2) a.push(n); }); return a; }
  function parentStack(g) { for (let p = g.parent; p; p = p.parent) if (stacked(p) && p.children.length >= 2) return p; return null; }
  function childStacks(S) {
    const a = [];
    (function w(n) {
      if (n.kind !== "group") return;
      if (stacked(n) && n.children.length >= 2) { a.push(n); return; }
      for (const c of n.children) w(c);
    })(selectedChild(S));
    return a;
  }

  // ---- node factory ----
  // Build a group over kids WITHOUT rewiring their parent pointers. Callers that
  // wrap an existing node (e.g. _replaceNode(s, _newGroup(o, s, nl))) rely on that
  // node's parent still pointing at its ORIGINAL enclosing group until the
  // structural op finishes; _resolve() calls rebuildParents to make every parent
  // pointer consistent before any layout or navigation reads it.
  function newNodeGroup(o, kids) {
    return { handle: 0, kind: "group", ref: "", parent: null, children: kids.slice(), orient: o, mode: Normal, pin: { amount: 0, has: false, enforced: false }, selected: false };
  }

  class Viewport {
    constructor(w, h) {
      this.root = null; this.w = w; this.h = h; this.caretX = 0; this.caretY = 0;
      this.focus = null; this.lens = null; this.visCur = null;
      this.handles = new Map(); this.nextH = 0; this.dirty = true;
    }
    _newLeaf(ref) {
      this.nextH++;
      const n = { handle: this.nextH, kind: "leaf", ref: ref || "", parent: null, children: [], minW: defMinW, minH: defMinH, natW: defNatW, natH: defNatH, priority: 0, orient: LTR, mode: Normal, pin: { amount: 0, has: false, enforced: false }, selected: false };
      this.handles.set(n.handle, n);
      return n;
    }
    _newGroup(o, ...kids) { return newNodeGroup(o, kids); }
    _tile(h) { const n = this.handles.get(h); return n && n.kind === "leaf" ? n : null; }
    _touch() { this.dirty = true; }

    // ---- render / query surface ----
    SetWorkspace(width, height) { this.w = width; this.h = height; this._touch(); }
    Caret() { this._ensure(); return { X: this.caretX, Y: this.caretY }; }
    Tiles() {
      this._ensure();
      const out = [];
      walk(this.root, n => {
        if (n.kind !== "leaf" || n.hidden) return;
        out.push({ Tile: n.handle, Ref: n.ref, Rect: { X: n.rect.X, Y: n.rect.Y, W: n.rect.W, H: n.rect.H }, Mode: n.mode, Pinned: n.pin.has, Selected: n.selected, Priority: n.priority });
      });
      return out;
    }
    Stacks() {
      this._ensure();
      const out = [];
      walk(this.root, n => {
        if (n.kind !== "group" || !stacked(n) || n.hidden) return;
        const tabs = n.children.map(c => { const l = focusEntry(c); return { Tile: l ? l.handle : 0, Ref: l ? l.ref : "", Selected: c.selected }; });
        out.push({ Rect: { X: n.rect.X, Y: n.rect.Y, W: n.rect.W, H: n.rect.H }, HeaderH: n.headerH || headerH, Tabs: tabs });
      });
      return out;
    }
    Reveal(tile) {
      const t = this._tile(tile); if (!t) return 0;
      for (let n = t; n && n.parent; n = n.parent) if (stacked(n.parent) && !n.selected) selectChild(n.parent, n);
      this._touch(); return tile;
    }
    SetMetrics(tile, minW, minH, natW, natH) {
      const n = this._tile(tile); if (!n) return;
      if (minW > 0) n.minW = minW; if (minH > 0) n.minH = minH; if (natW > 0) n.natW = natW; if (natH > 0) n.natH = natH;
      this._touch();
    }
    SetPriority(tile, p) { const n = this._tile(tile); if (n) { n.priority = p; this._touch(); } }

    // ---- refs ----
    Content(tile) { const n = this._tile(tile); return n ? n.ref : ""; }
    Set(tile, ref) { const n = this._tile(tile); if (n) n.ref = ref; }
    Get(ref, includeHidden) {
      if (!includeHidden) this._ensure();
      const out = [];
      walk(this.root, n => { if (n.kind === "leaf" && n.ref === ref && (includeHidden || !n.hidden)) out.push(n.handle); });
      return out;
    }

    // ---- resolve ----
    _ensure() { if (this.dirty) this._resolve(); }
    _resolve() {
      normalize(this.root);
      while (this.root.kind === "group" && this.root.children.length === 1 && this.root.children[0].kind === "group") this.root = this.root.children[0];
      if (this.root.kind === "leaf") this.root = this._newGroup(LTR, this.root);
      if (this.root.kind === "group" && this.root.children.length === 0) this.root.children = [this._newLeaf("")];
      this.root.selected = false;
      rebuildParents(this.root);
      measure(this.root); computeEff(this.root); allocate(this.root, 0, 0, this.w, this.h);
      walk(this.root, n => { n.navRect = n.rect; n.navHidden = n.hidden; });
      if (this.lens) {
        const X = this._lensTarget(this.lens.tile, this.lens.group);
        if (!X) this.lens = null;
        else if (this.lens.scope === "screen") { markHidden(this.root, "lens"); allocate(X, 0, 0, this.w, this.h); }
        else {
          const A = X.parent || this.root, r = A.rect;
          if (X.parent) for (const c of X.parent.children) if (c !== X) markHidden(c, "lens");
          allocate(X, r.X, r.Y, r.W, r.H);
        }
      }
      this.dirty = false;
    }

    // ---- structural ----
    _insertSib(g, ref, n, s) {
      let i = indexOf(g, ref); if (s === After) i++;
      g.children.splice(i, 0, n); n.parent = g;
    }
    _replaceNode(oldN, neu) {
      neu.selected = oldN.selected; oldN.selected = false;
      if (oldN === this.root) { this.root = neu; neu.parent = null; }
      else { const p = oldN.parent; p.children[indexOf(p, oldN)] = neu; neu.parent = p; }
    }
    _seekUpInsert(G, ad, d, nl) {
      let cur = G, parent = G.parent;
      while (parent) { if (axOf(parent) === ad) { this._insertSib(parent, cur, nl, side(parent.orient, d)); return; } cur = parent; parent = parent.parent; }
      const o = defOrient(ad);
      this._replaceNode(G, side(o, d) === Before ? this._newGroup(o, nl, G) : this._newGroup(o, G, nl));
    }
    _settle(focus) { this._touch(); this._ensure(); this._rehomeCaret(focus); }

    New(tile, d, ref) {
      const s = this._tile(tile); if (!s || !s.parent) return 0;
      const G = s.parent, r = ref !== undefined ? ref : s.ref, nl = this._newLeaf(r);
      if (d === Before || d === After) { this._insertSib(G, s, nl, d); this._settle(s); return nl.handle; }
      const ad = axisFor(d);
      if (axOf(G) === ad) {
        const sd = side(G.orient, d), i = indexOf(G, s);
        const atEnd = (sd === Before && i === 0) || (sd === After && i === G.children.length - 1);
        if (!atEnd) { this._insertSib(G, s, nl, sd); this._settle(s); return nl.handle; }
      }
      this._seekUpInsert(G, ad, d, nl); this._settle(s); return nl.handle;
    }
    Split(tile, d, ref) {
      const s = this._tile(tile); if (!s || !s.parent) return 0;
      const G = s.parent; let o, s2;
      if (d === Before || d === After) { o = axisOf(G.orient) === "H" ? defOrient("V") : defOrient("H"); s2 = d; }
      else { const ax = axisFor(d); o = ax === axisOf(G.orient) ? G.orient : defOrient(ax); s2 = side(o, d); }
      const r = ref !== undefined ? ref : s.ref, nl = this._newLeaf(r);
      this._replaceNode(s, s2 === Before ? this._newGroup(o, nl, s) : this._newGroup(o, s, nl));
      this._settle(s); return nl.handle;
    }
    Close(tile) {
      const s = this._tile(tile); if (!s) return 0;
      const next = nextFocusAfterDismiss(s);
      if (s.parent) removeNode(s.parent.children, s);
      this.handles.delete(s.handle);
      if (next) { this._settle(next); return next.handle; }
      this._touch(); this._ensure(); return 0;
    }
    Flip(tile) { const s = this._tile(tile); if (s && s.parent) { s.parent.orient = FLIP[s.parent.orient]; this._touch(); } }
    FlipParent(tile) { const s = this._tile(tile); if (s && s.parent && s.parent.parent) { s.parent.parent.orient = FLIP[s.parent.parent.orient]; this._touch(); } }
    Reverse(tile) { const s = this._tile(tile); if (s && s.parent) { reverse(s.parent.children); this._touch(); } }
    ReverseParent(tile) { const s = this._tile(tile); if (s && s.parent && s.parent.parent) { reverse(s.parent.parent.children); this._touch(); } }
    Stack(tile, state) {
      const s = this._tile(tile); if (!s || !s.parent) return;
      const g = s.parent, is = stacked(g);
      let want = !is; if (state === On) want = true; else if (state === Off) want = false;
      if (want && !is) { for (const c of g.children) c.selected = c === s; this._touch(); }
      else if (!want && is) { for (const c of g.children) c.selected = false; g.orient = contraryOrient(g); this._touch(); }
    }
    Swap(tile, d) {
      const src = opTargetOf(this._tile(tile)); if (!src) return;
      this._ensure();
      const t = this._navTarget(src, d);
      if (!t || t === src) { this._caretToEdge(src, d); return; }
      const vert = d === Up || d === Down, keep = vert ? this.caretY : this.caretX;
      swapNodes(src, t);
      [src.pin, t.pin] = [t.pin, src.pin];
      [src.mode, t.mode] = [t.mode, src.mode];
      [src.selected, t.selected] = [t.selected, src.selected];
      this._touch(); this._ensure();
      const r = src.rect, ins = 8;
      if (vert) { this.caretX = clamp(keep, r.X + ins, r.X + r.W - ins); this.caretY = r.Y + r.H / 2; }
      else { this.caretY = clamp(keep, r.Y + ins, r.Y + r.H - ins); this.caretX = r.X + r.W / 2; }
    }
    SwapUp(t) { this.Swap(t, Up); } SwapDown(t) { this.Swap(t, Down); } SwapLeft(t) { this.Swap(t, Left); } SwapRight(t) { this.Swap(t, Right); }
    Merge(tile, d) {
      const src = this._tile(tile); if (!src) return;
      this._ensure();
      const ad = axisFor(d), pd = ad === "V" ? "H" : "V", D = this._navTarget(src, d);
      if (D === src) return;
      const g = src.parent;
      if (g && stacked(g) && src.selected) {
        const i = indexOf(g, src); const nb = i + 1 < g.children.length ? g.children[i + 1] : (i - 1 >= 0 ? g.children[i - 1] : null);
        if (nb) selectChild(g, nb);
      }
      if (!D) { this._mergeToEdge(src, d, ad); return; }
      const dst = destStack(D, src);
      detach(src);
      if (dst) { dst.children.push(src); src.parent = dst; }
      else {
        let phys;
        if (pd === "H") phys = this.caretX <= D.rect.X + D.rect.W / 2 ? Left : Right;
        else phys = this.caretY <= D.rect.Y + D.rect.H / 2 ? Up : Down;
        const p = D.parent;
        if (p && !stacked(p) && axisOf(p.orient) === pd) this._insertSib(p, D, src, side(p.orient, phys));
        else { const o = defOrient(pd); this._replaceNode(D, side(o, phys) === Before ? this._newGroup(o, src, D) : this._newGroup(o, D, src)); }
      }
      this._settle(src);
    }
    MergeUp(t) { this.Merge(t, Up); } MergeDown(t) { this.Merge(t, Down); } MergeLeft(t) { this.Merge(t, Left); } MergeRight(t) { this.Merge(t, Right); }
    _mergeToEdge(s, d, ad) {
      if (leavesOf(this.root).length < 2) return;
      detach(s);
      if (axisOf(this.root.orient) === ad) { if (side(this.root.orient, d) === Before) this.root.children.unshift(s); else this.root.children.push(s); s.parent = this.root; }
      else { const o = defOrient(ad); this.root = side(o, d) === Before ? this._newGroup(o, s, this.root) : this._newGroup(o, this.root, s); }
      this._settle(s);
    }
    MoveTabNext(tile) { this._moveTab(tile, 1); }
    MoveTabPrior(tile) { this._moveTab(tile, -1); }
    _moveTab(tile, step) {
      const g = enclosingStack(this._tile(tile)); if (!g) return;
      const i = indexOf(g, selectedChild(g)), j = i + step;
      if (j < 0 || j >= g.children.length) return;
      [g.children[i], g.children[j]] = [g.children[j], g.children[i]];
      this._touch();
    }

    // ---- navigation ----
    _landingLeaf(cur, d) {
      const navH = d === Left || d === Right;
      while (cur && cur.kind === "group") {
        const kids = cur.children.filter(c => !c.navHidden);
        if (!kids.length) return null;
        if ((axisOf(cur.orient) === "H") === navH) {
          let pick = kids[0];
          for (const c of kids.slice(1)) {
            const R = c.navRect, P = pick.navRect;
            if (d === Right && R.X < P.X) pick = c;
            else if (d === Left && R.X + R.W > P.X + P.W) pick = c;
            else if (d === Down && R.Y < P.Y) pick = c;
            else if (d === Up && R.Y + R.H > P.Y + P.H) pick = c;
          }
          cur = pick;
        } else {
          const g = navH ? this.caretY : this.caretX;
          const lo = c => navH ? c.navRect.Y : c.navRect.X, hi = c => navH ? c.navRect.Y + c.navRect.H : c.navRect.X + c.navRect.W;
          let pick = kids.find(c => g >= lo(c) - 0.5 && g <= hi(c) + 0.5);
          if (!pick) { pick = kids[0]; let bd = gap(lo(pick), hi(pick), g); for (const c of kids.slice(1)) { const gp = gap(lo(c), hi(c), g); if (gp < bd) { bd = gp; pick = c; } } }
          cur = pick;
        }
      }
      return cur;
    }
    _navTarget(from, d) {
      const navH = d === Left || d === Right, want = navH ? "H" : "V";
      let node = from, parent = from.parent;
      while (parent) {
        if (axOf(parent) === want) {
          const i = indexOf(parent, node), step = side(parent.orient, d) === Before ? -1 : 1;
          for (let ni = i + step; ni >= 0 && ni < parent.children.length; ni += step) { const cand = this._landingLeaf(parent.children[ni], d); if (cand) return cand; }
        }
        node = parent; parent = parent.parent;
      }
      return null;
    }
    _caretToEdge(cur, d) {
      const b = cur.navRect, horiz = d === Left || d === Right;
      const lo = horiz ? b.X : b.Y, size = horiz ? b.W : b.H, hi = lo + size, mid = lo + size / 2, third = size / 3;
      const pos = horiz ? this.caretX : this.caretY;
      let np;
      if (d === Right || d === Down) np = pos <= lo + third ? mid : hi;
      else np = pos >= hi - third ? mid : lo;
      if (horiz) this.caretX = np; else this.caretY = np;
    }
    _rehomeCaret(n) {
      if (!n) return; const r = n.rect;
      if (this.caretX < r.X || this.caretX > r.X + r.W) this.caretX = r.X + r.W / 2;
      if (this.caretY < r.Y || this.caretY > r.Y + r.H) this.caretY = r.Y + r.H / 2;
    }
    Go(tile, d) {
      const from = this._tile(tile); if (!from) return 0;
      this._ensure();
      if (d === Prior || d === Next) {
        const dest = this._readingCycle(from, d);
        if (dest) { this._rehomeCaret(dest); return dest.handle; }
        return tile;
      }
      const t = this._navTarget(from, d);
      if (!t) { this._caretToEdge(from, d); return tile; }
      const b = t.navRect;
      if (d === Left || d === Right) this.caretX = b.X + b.W / 2; else this.caretY = b.Y + b.H / 2;
      return t.handle;
    }
    _readingCycle(from, d) {
      const ls = leavesOf(this.root); if (!ls.length) return null;
      let i = ls.indexOf(from); if (i < 0) i = 0;
      const step = d === Prior ? -1 : 1;
      return ls[((i + step) % ls.length + ls.length) % ls.length];
    }
    Up(t) { return this.Go(t, Up); } Down(t) { return this.Go(t, Down); } Left(t) { return this.Go(t, Left); } Right(t) { return this.Go(t, Right); }
    Prior(t) { return this.Go(t, Prior); } Next(t) { return this.Go(t, Next); }
    SetCaret(tile, x, y) {
      const n = this._tile(tile); if (!n) return;
      this._ensure(); const r = n.navRect;
      this.caretX = r.X + clamp(x, 0, r.W); this.caretY = r.Y + clamp(y, 0, r.H);
    }
    GetTile(x, y) {
      this._ensure(); let hit = 0;
      walk(this.root, n => { if (n.kind !== "leaf" || n.hidden) return; const r = n.rect; if (x >= r.X && x <= r.X + r.W && y >= r.Y && y <= r.Y + r.H) hit = n.handle; });
      return hit;
    }

    // ---- sizing ----
    _negFrom(tile) { const n = this._tile(tile); return n ? climbTiling(n) : null; }
    _manualPin(n, size) {
      const wasZoom = n.mode === Zoom;
      let minMain = n.parent && isH(n.parent.orient) ? n.mMinW : n.mMinH; if (minMain <= 0) minMain = 10;
      if (minMain > size) size = minMain;
      n.pin = { amount: size, has: true, enforced: true }; n.mode = Normal;
      if (wasZoom && n.parent && !groupHasZoom(n.parent)) restoreInactive(n.parent);
      this._touch();
    }
    Zoom(tile, state) { this._modeToggle(tile, Zoom, state); }
    Shrink(tile, state) { this._modeToggle(tile, Shrink, state); }
    Normal(tile) { const t = this._negFrom(tile); if (t) { setMode(t, Normal); this._touch(); } }
    ModeNext(tile) { const t = this._negFrom(tile); if (t) { setMode(t, cycleModeVal(t.mode, 1)); this._touch(); } }
    ModePrior(tile) { const t = this._negFrom(tile); if (t) { setMode(t, cycleModeVal(t.mode, -1)); this._touch(); } }
    _modeToggle(tile, m, state) {
      const t = this._negFrom(tile); if (!t) return;
      let target = Normal;
      if (state === On) target = m; else if (state === Off) target = Normal; else if (t.mode !== m) target = m;
      setMode(t, target); this._touch();
    }
    Expand(tile, delta) { this._resizeBy(tile, delta === undefined ? 1 : delta); }
    Contract(tile, delta) { this._resizeBy(tile, -(delta === undefined ? 1 : delta)); }
    _resizeBy(tile, delta) {
      const t = this._negFrom(tile); if (!t) return;
      this._ensure(); if (t.hidden) return;
      const cur = t.parent && isH(t.parent.orient) ? t.rect.W : t.rect.H;
      this._manualPin(t, cur + delta);
    }
    Resize(tile, size) {
      const t = this._negFrom(tile); if (!t) return;
      if (size === undefined || size <= 0) { clearPin(t); this._touch(); return; }
      this._ensure(); this._manualPin(t, size);
    }
    Equalize(tile, recursive) { const t = this._negFrom(tile); if (!t) return; this._ensure(); this._eachTiling(baseGroup(t), !!recursive, g => this._equalizeGroup(g)); this._touch(); }
    Balance(tile, recursive) { const t = this._negFrom(tile); if (!t) return; this._eachTiling(baseGroup(t), !!recursive, balanceGroup); this._touch(); }
    _equalizeGroup(g) {
      if (g.kind !== "group" || stacked(g) || !g.children.length) return;
      const main = isH(g.orient) ? g.rect.W : g.rect.H, share = main / g.children.length;
      for (const c of g.children) { c.mode = Normal; c.pin = { amount: share, has: true, enforced: true }; }
    }
    _eachTiling(g, recursive, fn) {
      fn(g);
      if (!recursive) return;
      const kids = stacked(g) ? (selectedChild(g) ? [selectedChild(g)] : []) : g.children;
      for (const c of kids) if (c.kind === "group") this._eachTiling(c, recursive, fn);
    }

    // ---- lenses ----
    _lensTarget(tile, group) { const t = this._negFrom(tile); if (!t) return null; return group && t.parent ? climbTiling(t.parent) : t; }
    Monocle(tile, state) { this._setLens(tile, false, "screen", state); }
    LocalMonocle(tile, state) { this._setLens(tile, false, "group", state); }
    Spectacle(tile, state) { this._setLens(tile, true, "screen", state); }
    LocalSpectacle(tile, state) { this._setLens(tile, true, "group", state); }
    _setLens(tile, group, scope, state) {
      const same = this.lens && this.lens.tile === tile && this.lens.group === group && this.lens.scope === scope;
      if (state === On) this.lens = { tile, group, scope };
      else if (state === Off) { if (same) this.lens = null; }
      else this.lens = same ? null : { tile, group, scope };
      this._touch();
    }
    SetFocus(tile) {
      const n = this._tile(tile); if (!n) return;
      this.focus = n;
      if (this.lens) { const X = this._lensTarget(this.lens.tile, this.lens.group); if (!X || !isAncestorOf(X, n)) { this.lens = null; this._touch(); } }
    }
    GetFocus() { return this.focus && this.handles.has(this.focus.handle) ? this.focus.handle : 0; }

    // ---- tabs ----
    TabNext(tile) { return this._tabCycle(tile, 1); }
    TabPrior(tile) { return this._tabCycle(tile, -1); }
    _goTab(g, j) { selectChild(g, g.children[j]); this._touch(); const l = focusEntry(g.children[j]); return l ? l.handle : 0; }
    _tabCycle(tile, step) {
      const s = this._tile(tile); if (!s) return 0;
      const stacks = []; for (let g = enclosingStack(s); g; g = enclosingStack(g)) stacks.push(g);
      for (const g of stacks) { const i = indexOf(g, selectedChild(g)), j = i + step; if (j >= 0 && j < g.children.length) return this._goTab(g, j); }
      if (stacks.length) { const top = stacks[stacks.length - 1], n = top.children.length; if (n < 2) return tile; const i = indexOf(top, selectedChild(top)); return this._goTab(top, ((i + step) % n + n) % n); }
      this._ensure(); this._cycleVisibleStack(step); this._touch(); return tile;
    }
    _cycleVisibleStack(step) {
      const vs = visStacksOf(this.root); if (!vs.length) return;
      const dir = step > 0 ? 1 : -1;
      const shown = g => indexOf(g, selectedChild(g)), set = (g, i) => selectChild(g, g.children[i]);
      const entry = h => step > 0 ? 0 : h.children.length - 1;
      const self = this;
      const descend = S => { let cur = S; for (; ;) { const ks = childStacks(cur); if (!ks.length) { self.visCur = cur; return; } const nx = step > 0 ? ks[0] : ks[ks.length - 1]; set(nx, entry(nx)); cur = nx; } };
      const enter = S => {
        let cur = S, moved = false;
        for (; ;) {
          const before = shown(cur), e = entry(cur);
          if (before !== e) { set(cur, e); moved = true; }
          let ks = childStacks(cur);
          if (!ks.length && !moved) { set(cur, e + step); moved = true; ks = childStacks(cur); }
          if (!ks.length) { self.visCur = cur; return; }
          cur = step > 0 ? ks[0] : ks[ks.length - 1];
        }
      };
      let D = this.visCur ? vs.find(s => s === this.visCur) : null;
      if (!D) { const top = vs.filter(s => parentStack(s) === null); if (!top.length) return; enter(step > 0 ? top[0] : top[top.length - 1]); return; }
      for (let g = D; ;) {
        const j = shown(g) + step;
        if (j >= 0 && j < g.children.length) { set(g, j); descend(g); return; }
        const par = parentStack(g), peers = vs.filter(s => parentStack(s) === par), i = indexOfNode(peers, g);
        if (par === null) { const m = peers.length; enter(peers[((i + dir) % m + m) % m]); return; }
        const ni = i + dir; if (ni >= 0 && ni < peers.length) { enter(peers[ni]); return; }
        g = par;
      }
    }
  }

  function gap(lo, hi, g) { let d = 0; if (lo - g > d) d = lo - g; if (g - hi > d) d = g - hi; return d; }
  function detach(n) { if (n.parent) removeNode(n.parent.children, n); n.selected = false; }

  // NewViewport creates a viewport with one initial tile; returns [viewport, handle].
  function NewViewport(width, height) {
    const v = new Viewport(width, height);
    const first = v._newLeaf("");
    v.root = { handle: 0, kind: "group", ref: "", parent: null, children: [first], orient: LTR, mode: Normal, pin: { amount: 0, has: false, enforced: false }, selected: false };
    first.parent = v.root;
    return [v, first.handle];
  }

  return {
    NewViewport, Viewport,
    Up, Down, Left, Right, Before, After, Prior, Next,
    Normal, Zoom, Shrink, Toggle, On, Off, LTR, RTL, TTB, BTT,
  };
});
