import { useEffect, useLayoutEffect, useRef, useState } from 'react';

export interface MenuItem {
  label?: string;
  onClick?: () => void;
  separator?: boolean;
  destructive?: boolean;
  disabled?: boolean;
}

interface Props {
  x: number;
  y: number;
  items: MenuItem[];
  onClose(): void;
}

export function ContextMenu({ x, y, items, onClose }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  // Keep the menu on screen when opened near an edge.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      left: Math.min(x, window.innerWidth - r.width - 8),
      top: Math.min(y, window.innerHeight - r.height - 8),
    });
  }, [x, y]);

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
