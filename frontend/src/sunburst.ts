// Sunburst geometry: layout, hit testing and animation, kept out of React so
// the canvas can be driven imperatively at 60fps.
import { VNode, ViewportSlice, FLAG_NOPERM } from './api';

export interface Arc {
  /** Node handle. 0 means the synthetic "other" wedge — never actionable. */
  id: number;
  /** Stable identity across layouts, so a zoom can animate arc to arc. */
  key: string;
  parentKey: string | null;
  name: string;
  size: number;
  logical: number;
  kind: number;
  flags: number;
  files: number;
  dirs: number;
  other: number;
  err?: string;
  /** Ring index: 0 is the first ring outside the centre disc. */
  depth: number;
  a0: number;
  a1: number;
  /** Index of the top-level branch this arc descends from, for hue. */
  hueIndex: number;
  /** Display path relative to the scan root, for the centre readout. */
  label: string;
  hasKids: boolean;
}

export interface Layout {
  arcs: Arc[];
  /** Arcs bucketed by ring, each sorted by a0, for O(log n) hit testing. */
  rings: Arc[][];
  focus: VNode;
  focusLabel: string;
  total: number;
  maxDepth: number;
}

const TAU = Math.PI * 2;

/**
 * layout turns a viewport slice into drawable arcs.
 *
 * Children are normalised against the sum of what came back rather than the
 * parent's own total. The two differ by the parent directory's own inode
 * bytes; normalising against the sum makes each ring close exactly, which is
 * what the eye expects, at a cost far below one pixel of angle.
 */
export function layout(slice: ViewportSlice, rootLabel: string): Layout {
  const arcs: Arc[] = [];
  const rings: Arc[][] = [];

  // Tolerate a payload whose optional collections are missing: an empty scan,
  // an older backend, or a focus at the root all produce one.
  const ancestors = slice.ancestors ?? [];
  // Joined with the platform's separator so the readout matches the paths the
  // user sees everywhere else.
  const sep = navigator.userAgent.includes('Windows') ? '\\' : '/';
  const focusLabel = [rootLabel, ...ancestors.slice(1).map((a) => a.name), slice.focus.name]
    .filter(Boolean)
    .slice(1)
    .join(sep);

  function walk(
    node: VNode,
    depth: number,
    a0: number,
    a1: number,
    hueIndex: number,
    parentKey: string | null,
    label: string,
  ) {
    const kids = node.children;
    if (!kids || kids.length === 0) return;

    let sum = 0;
    for (const k of kids) sum += Math.max(k.size, 0);
    if (sum <= 0) return;

    const span = a1 - a0;
    let cursor = a0;
    for (let i = 0; i < kids.length; i++) {
      const k = kids[i];
      const frac = Math.max(k.size, 0) / sum;
      const start = cursor;
      const end = i === kids.length - 1 ? a1 : cursor + span * frac;
      cursor = end;
      if (end - start <= 0) continue;

      const key = k.id !== 0 ? `n${k.id}` : `o${parentKey ?? 'root'}@${depth}`;
      const childLabel = label ? `${label}/${k.name}` : k.name;
      const arc: Arc = {
        id: k.id,
        key,
        parentKey,
        name: k.name,
        size: k.size,
        logical: k.logical,
        kind: k.kind,
        flags: k.flags,
        files: k.files,
        dirs: k.dirs,
        other: k.other ?? 0,
        err: k.err,
        depth,
        a0: start,
        a1: end,
        // Top-level children seed the hue; everything below inherits it, so a
        // branch reads as one colour family all the way out.
        hueIndex: depth === 0 ? (k.other ? -1 : i) : k.other ? -1 : hueIndex,
        label: childLabel,
        hasKids: k.hasKids,
      };
      arcs.push(arc);
      (rings[depth] ??= []).push(arc);
      walk(k, depth + 1, start, end, arc.hueIndex, key, childLabel);
    }
  }

  walk(slice.focus, 0, -Math.PI / 2, -Math.PI / 2 + TAU, 0, null, '');

  return {
    arcs,
    rings,
    focus: slice.focus,
    focusLabel,
    total: slice.focus.size,
    maxDepth: rings.length,
  };
}

export interface Geometry {
  cx: number;
  cy: number;
  /** Radius of the centre disc. */
  r0: number;
  /** Width of one ring. */
  ringWidth: number;
  rings: number;
}

export function geometry(width: number, height: number, rings: number): Geometry {
  const cx = width / 2;
  const cy = height / 2;
  const outer = Math.max(40, Math.min(width, height) / 2 - 30);
  // The centre disc carries the readout, so it gets a generous share and the
  // remaining radius is split evenly between rings.
  const r0 = outer * 0.3;
  const ringWidth = (outer - r0) / Math.max(rings, 1);
  return { cx, cy, r0, ringWidth, rings };
}

/** hitTest maps a canvas point to an arc, or null for the centre or outside. */
export function hitTest(layout: Layout, geo: Geometry, x: number, y: number): Arc | null {
  const dx = x - geo.cx;
  const dy = y - geo.cy;
  const r = Math.hypot(dx, dy);
  if (r <= geo.r0) return null;
  const depth = Math.floor((r - geo.r0) / geo.ringWidth);
  const ring = layout.rings[depth];
  if (!ring || ring.length === 0) return null;

  // Canvas angles run clockwise from 3 o'clock; the layout starts at 12.
  let theta = Math.atan2(dy, dx);
  const base = -Math.PI / 2;
  while (theta < base) theta += TAU;
  while (theta >= base + TAU) theta -= TAU;

  let lo = 0;
  let hi = ring.length - 1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    const a = ring[mid];
    if (theta < a.a0) hi = mid - 1;
    else if (theta >= a.a1) lo = mid + 1;
    else return a;
  }
  return null;
}

/** isCentre reports whether a point is inside the centre disc. */
export function isCentre(geo: Geometry, x: number, y: number): boolean {
  return Math.hypot(x - geo.cx, y - geo.cy) <= geo.r0;
}

export interface Placed {
  a0: number;
  a1: number;
  depth: number;
  alpha: number;
}

/**
 * interpolate positions an arc part-way between two layouts.
 *
 * An arc present in both animates directly. A newly revealed arc starts
 * collapsed at the position its nearest visible ancestor occupied and fades
 * in, which is what makes zooming read as the chart unfolding rather than
 * being replaced.
 */
export function interpolate(
  arc: Arc,
  from: Map<string, Placed>,
  parentOf: Map<string, string | null>,
  t: number,
): Placed {
  const prev = from.get(arc.key);
  if (prev) {
    return {
      a0: prev.a0 + (arc.a0 - prev.a0) * t,
      a1: prev.a1 + (arc.a1 - prev.a1) * t,
      depth: prev.depth + (arc.depth - prev.depth) * t,
      alpha: 1,
    };
  }
  // Walk up to the nearest ancestor that existed before, and grow out of the
  // wedge it occupied.
  let key: string | null = arc.parentKey;
  let hops = 1;
  while (key) {
    const anc = from.get(key);
    if (anc) {
      const mid = (anc.a0 + anc.a1) / 2;
      const startDepth = anc.depth + hops;
      return {
        a0: mid + (arc.a0 - mid) * t,
        a1: mid + (arc.a1 - mid) * t,
        depth: startDepth + (arc.depth - startDepth) * t,
        alpha: t,
      };
    }
    key = parentOf.get(key) ?? null;
    hops++;
  }
  // Nothing in common with the previous layout: fade in where it belongs.
  return { a0: arc.a0, a1: arc.a1, depth: arc.depth, alpha: t };
}

/** parentMap lets the interpolator climb to an arc's nearest known ancestor. */
export function parentMap(layout: Layout): Map<string, string | null> {
  const m = new Map<string, string | null>();
  for (const a of layout.arcs) m.set(a.key, a.parentKey);
  return m;
}

export function snapshotPositions(layout: Layout): Map<string, Placed> {
  const m = new Map<string, Placed>();
  for (const a of layout.arcs) m.set(a.key, { a0: a.a0, a1: a.a1, depth: a.depth, alpha: 1 });
  return m;
}

/** easeInOutCubic: quick to leave, gentle to arrive. */
export function ease(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

export function isInaccessible(arc: Arc): boolean {
  return (arc.flags & FLAG_NOPERM) !== 0;
}
