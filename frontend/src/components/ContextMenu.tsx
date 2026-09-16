import { useEffect, useLayoutEffect, useRef, useState } from 'react';

export interface MenuItem {
  label?: string;
  onClick?: () => void;
  separator?: boolean;
  destructive?: boolean;
  disabled?: boolean;
}

interface Props {
  /** Viewport coordinates, as a mouse event reports them. */
  x: number;
  y: number;
  /** Interface zoom, needed to place a fixed element inside a zoomed root. */
  scale: number;
  items: MenuItem[];
  onClose(): void;
}

export function ContextMenu({ x, y, scale, items, onClose }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState({ left: x / (scale || 1), top: y / (scale || 1) });

  // A mouse event reports viewport pixels, but `position: fixed` inside a
  // zoomed root is laid out in the zoomed coordinate space — so at 150% an
  // unconverted click coordinate put the menu half again too far down and
  // right, off screen entirely at the edges. Everything here works in layout
  // space, including the bounds it is clamped against.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const z = scale || 1;
    const r = el.getBoundingClientRect();
    const menuW = r.width / z;
    const menuH = r.height / z;
    const viewW = window.innerWidth / z;
    const viewH = window.innerHeight / z;
    setPos({
      left: Math.max(4, Math.min(x / z, viewW - menuW - 8)),
      top: Math.max(4, Math.min(y / z, viewH - menuH - 8)),
    });
  }, [x, y, scale]);

  useEffect(() => {
    const close = () => onClose();
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('mousedown', close);
    window.addEventListener('resize', close);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('mousedown', close);
      window.removeEventListener('resize', close);
      window.removeEventListener('keydown', key);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      className="ctxmenu"
      style={{ left: pos.left, top: pos.top }}
      onMouseDown={(e) => e.stopPropagation()}
      role="menu"
    >
      {items.map((it, i) =>
        it.separator ? (
          <div key={i} className="ctx-sep" role="separator" />
        ) : (
          <button
            key={i}
            role="menuitem"
            className={'ctx-item' + (it.destructive ? ' ctx-danger' : '')}
            disabled={it.disabled}
            onClick={() => {
              it.onClick?.();
              onClose();
            }}
          >
            {it.label}
          </button>
        ),
      )}
    </div>
  );
}
