// The radial chart. The canvas is drawn imperatively from a layout held in a
// ref; React only owns the surrounding chrome, so a 250ms scan snapshot never
// costs a reconciliation.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ViewportSlice } from '../api';
import { arcColor, centreColor, Palette } from '../colors';
import { formatBytes, formatCount, formatPercent } from '../format';
import {
  Arc,
  ease,
  geometry,
  Geometry,
  hitTest,
  interpolate,
  isCentre,
  isInaccessible,
  Layout,
  layout as buildLayout,
  parentMap,
  Placed,
  snapshotPositions,
} from '../sunburst';

const ZOOM_MS = 300;

interface Props {
  slice: ViewportSlice | undefined;
  rootLabel: string;
  palette: Palette;
  dark: boolean;
  base2: boolean;
  rings: number;
  hovered: number;
  selected: number;
  scanning: boolean;
  onHover(arc: Arc | null): void;
  onSelect(arc: Arc): void;
  onZoom(arc: Arc): void;
  onZoomOut(): void;
  onContextMenu(arc: Arc, x: number, y: number): void;
}

export function Sunburst(props: Props) {
  const {
    slice, rootLabel, palette, dark, base2, rings,
    hovered, selected, scanning,
    onHover, onSelect, onZoom, onZoomOut, onContextMenu,
  } = props;

  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [size, setSize] = useState({ w: 600, h: 600 });

  // Everything the draw loop reads lives in refs: state updates would throw
  // away frames during a scan.
  const layoutRef = useRef<Layout | null>(null);
  const fromRef = useRef<Map<string, Placed>>(new Map());
  const parentRef = useRef<Map<string, string | null>>(new Map());
  const animStartRef = useRef(0);
  const rafRef = useRef(0);
  const hoverRef = useRef<Arc | null>(null);
  const focusKeyRef = useRef<string>('');
  const hoverAncestors = useRef<Set<string>>(new Set());

  const themeRef = useRef({ palette, dark, base2, hovered, selected, rings });
  themeRef.current = { palette, dark, base2, hovered, selected, rings };

  // ---- sizing ------------------------------------------------------------
  useLayoutEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect();
      setSize({ w: Math.max(240, r.width), h: Math.max(240, r.height) });
    });
    ro.observe(el);
    const r = el.getBoundingClientRect();
    setSize({ w: Math.max(240, r.width), h: Math.max(240, r.height) });
    return () => ro.disconnect();
  }, []);

  // ---- layout ------------------------------------------------------------
  useEffect(() => {
    if (!slice) return;
    const next = buildLayout(slice, rootLabel);
    const focusKey = `n${slice.focus.id}`;
    const changedFocus = focusKey !== focusKeyRef.current;

    if (changedFocus && layoutRef.current) {
      // Zooming: animate from where things were.
      fromRef.current = snapshotPositions(layoutRef.current);
      parentRef.current = parentMap(layoutRef.current);
      animStartRef.current = performance.now();
    } else if (!layoutRef.current) {
      fromRef.current = new Map();
      parentRef.current = new Map();
      animStartRef.current = performance.now();
    } else {
      // A scan snapshot at the same focus: snap, do not animate. Re-running a
      // 300ms zoom every 250ms would make the chart appear to wobble.
      animStartRef.current = 0;
    }
    focusKeyRef.current = focusKey;
    layoutRef.current = next;
    schedule();
  }, [slice, rootLabel]);

  // ---- drawing -----------------------------------------------------------
  const draw = useCallback(() => {
    rafRef.current = 0;
    const canvas = canvasRef.current;
    const lay = layoutRef.current;
    if (!canvas || !lay) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const { palette, dark, base2, selected } = themeRef.current;
    const dpr = window.devicePixelRatio || 1;
    const { w, h } = { w: canvas.clientWidth, h: canvas.clientHeight };
    if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const geo = geometry(w, h, Math.max(lay.maxDepth, 1));
    const start = animStartRef.current;
    const t = start === 0 ? 1 : Math.min(1, (performance.now() - start) / ZOOM_MS);
    const e = ease(t);

    const hov = hoverRef.current;
    const hoverKeys = hoverAncestors.current;

    for (const arc of lay.arcs) {
      const p = start === 0
        ? { a0: arc.a0, a1: arc.a1, depth: arc.depth, alpha: 1 }
        : interpolate(arc, fromRef.current, parentRef.current, e);
      if (p.alpha <= 0.01) continue;

      const rIn = geo.r0 + p.depth * geo.ringWidth;
      const rOut = rIn + geo.ringWidth;
      if (rOut <= rIn || p.a1 - p.a0 <= 0.00015) continue;

      const isHot = hov ? hoverKeys.has(arc.key) : false;
      const style = arcColor(palette, arc.hueIndex, Math.round(p.depth), arc.kind, arc.flags, isHot, dark);

      ctx.globalAlpha = p.alpha;
      ctx.beginPath();
      ctx.arc(geo.cx, geo.cy, rIn, p.a0, p.a1);
      ctx.arc(geo.cx, geo.cy, rOut, p.a1, p.a0, true);
      ctx.closePath();
      ctx.fillStyle = style.fill;
      ctx.fill();

      if (isInaccessible(arc)) hatch(ctx, geo, rIn, rOut, p.a0, p.a1, dark);

      // Hairlines only when the wedge is wide enough to show one; below that
      // the strokes merge into a grey band and the chart loses its colour.
      if (p.a1 - p.a0 > 0.012 && geo.ringWidth > 4) {
        ctx.lineWidth = 1;
        ctx.strokeStyle = style.stroke;
        ctx.stroke();
      }
      if (arc.id !== 0 && arc.id === selected) {
        ctx.lineWidth = 2.5;
        ctx.strokeStyle = dark ? '#ffffff' : '#111318';
        ctx.stroke();
      }
    }
    ctx.globalAlpha = 1;

    drawCentre(ctx, geo, lay, hov, dark, base2);

    if (t < 1) schedule();
  }, []);

  const schedule = useCallback(() => {
    if (rafRef.current) return;
    rafRef.current = requestAnimationFrame(draw);
  }, [draw]);

  useEffect(() => {
    schedule();
  }, [size, palette, dark, base2, selected, hovered, schedule]);

  useEffect(
    () => () => {
      // Clearing the handle matters as much as cancelling the frame: schedule()
      // treats a non-zero handle as "a frame is already queued", so leaving a
      // cancelled id behind wedges the draw loop shut for good. StrictMode
      // mounts, cleans up and remounts, so this fires on every startup.
      cancelAnimationFrame(rafRef.current);
      rafRef.current = 0;
    },
    [],
  );

  // Hover driven from outside (the sidebar) must light the same arc.
  useEffect(() => {
    const lay = layoutRef.current;
    if (!lay) return;
    if (hovered === 0) {
      if (hoverRef.current) {
        hoverRef.current = null;
        hoverAncestors.current = new Set();
        schedule();
      }
      return;
    }
    const arc = lay.arcs.find((a) => a.id === hovered);
    if (arc && arc !== hoverRef.current) {
      setHover(arc);
    }
  }, [hovered, schedule]);

  function setHover(arc: Arc | null) {
    hoverRef.current = arc;
    const keys = new Set<string>();
    if (arc) {
      // Light the hovered arc and everything under it, so a hovered folder
      // shows the whole branch it owns.
      keys.add(arc.key);
      const lay = layoutRef.current;
      if (lay) {
        let grew = true;
        while (grew) {
          grew = false;
          for (const a of lay.arcs) {
            if (!keys.has(a.key) && a.parentKey && keys.has(a.parentKey)) {
              keys.add(a.key);
              grew = true;
            }
          }
        }
      }
    }
    hoverAncestors.current = keys;
    schedule();
  }

  // ---- pointer -----------------------------------------------------------
  function localPoint(ev: React.MouseEvent): { x: number; y: number; geo: Geometry } | null {
    const canvas = canvasRef.current;
    const lay = layoutRef.current;
    if (!canvas || !lay) return null;
    const r = canvas.getBoundingClientRect();
    const geo = geometry(r.width, r.height, Math.max(lay.maxDepth, 1));
    return { x: ev.clientX - r.left, y: ev.clientY - r.top, geo };
  }

  function handleMove(ev: React.MouseEvent) {
    const pt = localPoint(ev);
    const lay = layoutRef.current;
    if (!pt || !lay) return;
    const arc = hitTest(lay, pt.geo, pt.x, pt.y);
    if (arc !== hoverRef.current) {
      setHover(arc);
      onHover(arc);
    }
  }

  function handleLeave() {
    setHover(null);
    onHover(null);
  }

  function handleClick(ev: React.MouseEvent) {
    const pt = localPoint(ev);
    const lay = layoutRef.current;
    if (!pt || !lay) return;
    if (isCentre(pt.geo, pt.x, pt.y)) {
      onZoomOut();
      return;
    }
    const arc = hitTest(lay, pt.geo, pt.x, pt.y);
    if (!arc) return;
    if (ev.detail >= 2 && arc.id !== 0 && arc.hasKids) onZoom(arc);
    else onSelect(arc);
  }

  function handleContext(ev: React.MouseEvent) {
    ev.preventDefault();
    const pt = localPoint(ev);
    const lay = layoutRef.current;
    if (!pt || !lay) return;
    const arc = hitTest(lay, pt.geo, pt.x, pt.y);
    if (arc) onContextMenu(arc, ev.clientX, ev.clientY);
  }

  return (
    <div className="sunburst-wrap" ref={wrapRef}>
      <canvas
        ref={canvasRef}
        className="sunburst"
        style={{ width: size.w, height: size.h }}
        onMouseMove={handleMove}
        onMouseLeave={handleLeave}
        onClick={handleClick}
        onContextMenu={handleContext}
        role="img"
        aria-label={
          slice
            ? `Disk usage chart. ${slice.focus.name}, ${formatBytes(slice.focus.size, base2)}, ${formatCount(slice.focus.files)} files.`
            : 'Disk usage chart'
        }
      />
      {scanning && <div className="sunburst-scanning">scanning…</div>}
    </div>
  );
}

// hatch marks a tree the scan was refused. Colouring it like data would claim
// knowledge of contents we were never allowed to read.
function hatch(
  ctx: CanvasRenderingContext2D,
  geo: Geometry,
  rIn: number,
  rOut: number,
  a0: number,
  a1: number,
  dark: boolean,
) {
  ctx.save();
  ctx.clip();
  ctx.strokeStyle = dark ? 'rgba(255,255,255,0.22)' : 'rgba(0,0,0,0.18)';
  ctx.lineWidth = 1;
  const step = 6;
  const x0 = geo.cx - rOut;
  const y0 = geo.cy - rOut;
  for (let i = 0; i < rOut * 4; i += step) {
    ctx.beginPath();
    ctx.moveTo(x0 + i, y0);
    ctx.lineTo(x0 + i - rOut * 2, y0 + rOut * 2);
    ctx.stroke();
  }
  ctx.restore();
}

function drawCentre(
  ctx: CanvasRenderingContext2D,
  geo: Geometry,
  lay: Layout,
  hov: Arc | null,
  dark: boolean,
  base2: boolean,
) {
  ctx.beginPath();
  ctx.arc(geo.cx, geo.cy, geo.r0 - 1, 0, Math.PI * 2);
  ctx.fillStyle = centreColor(dark);
  ctx.fill();
  ctx.lineWidth = 1;
  ctx.strokeStyle = dark ? 'rgba(255,255,255,0.10)' : 'rgba(0,0,0,0.10)';
  ctx.stroke();

  const title = hov ? hov.name : lay.focus.name;
  const bytes = hov ? hov.size : lay.focus.size;
  const sub = hov
    ? hov.other > 0
      ? `${formatCount(hov.other)} smaller items`
      : hov.kind === 1 || hov.kind === 3
        ? `${formatCount(hov.files)} files`
        : ''
    : `${formatCount(lay.focus.files)} files`;
  const share = lay.total > 0 && hov ? formatPercent(hov.size / lay.total) : '';

  const fg = dark ? '#f2f3f7' : '#14161c';
  const dim = dark ? 'rgba(242,243,247,0.55)' : 'rgba(20,22,28,0.55)';
  const maxW = geo.r0 * 1.68;

  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';

  ctx.fillStyle = dim;
  ctx.font = '500 11px -apple-system, BlinkMacSystemFont, "SF Pro Text", sans-serif';
  const pathText = hov ? hov.label : lay.focusLabel;
  if (pathText) fitText(ctx, dirname(pathText), geo.cx, geo.cy - geo.r0 * 0.46, maxW);

  ctx.fillStyle = fg;
  ctx.font = '600 15px -apple-system, BlinkMacSystemFont, "SF Pro Text", sans-serif';
  fitText(ctx, title, geo.cx, geo.cy - geo.r0 * 0.16, maxW);

  ctx.font = '700 22px -apple-system, BlinkMacSystemFont, "SF Pro Display", sans-serif';
  ctx.fillText(formatBytes(bytes, base2), geo.cx, geo.cy + geo.r0 * 0.2);

  ctx.fillStyle = dim;
  ctx.font = '400 11px -apple-system, BlinkMacSystemFont, "SF Pro Text", sans-serif';
  const tail = [sub, share].filter(Boolean).join(' · ');
  if (tail) fitText(ctx, tail, geo.cx, geo.cy + geo.r0 * 0.5, maxW);
}

function dirname(p: string): string {
  const i = p.lastIndexOf('/');
  return i <= 0 ? '' : p.slice(0, i);
}

function fitText(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, maxW: number) {
  let t = text;
  if (ctx.measureText(t).width <= maxW) {
    ctx.fillText(t, x, y);
    return;
  }
  // Trim from the front: the tail of a path is the part that identifies it.
  while (t.length > 3 && ctx.measureText(`…${t}`).width > maxW) t = t.slice(1);
  ctx.fillText(`…${t}`, x, y);
}
