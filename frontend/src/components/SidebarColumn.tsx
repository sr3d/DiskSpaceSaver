// The right-hand column, with a drag handle on its leading edge.
//
// The width is a preference rather than component state so it survives closing
// a tab and restarting the app, but it is only written once the drag ends —
// persisting on every mouse move would mean a file write per pixel.
import { ReactNode, useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { useStore } from '../store';

const MIN = 220;
const MAX = 900;

interface Props {
  scanID: string;
  base2: boolean;
  uiScale: number;
  children: ReactNode;
}

export function SidebarColumn({ uiScale, children }: Props) {
  const stored = useStore((s) => s.prefs?.sidebarWidth ?? 350);
  const setPrefs = useStore((s) => s.setPrefs);
  const [width, setWidth] = useState(stored);
  const [dragging, setDragging] = useState(false);
  const startRef = useRef({ x: 0, w: 0 });

  // Follow the stored value when it changes elsewhere — Preferences, another
  // window — but not while this drag is the thing changing it.
  useEffect(() => {
    if (!dragging) setWidth(stored);
  }, [stored, dragging]);

  const onMove = useCallback(
    (e: MouseEvent) => {
      // Pointer deltas are in visual pixels and the width is a layout value;
      // at 150% interface scale an undivided delta drags half again too far.
      const delta = (startRef.current.x - e.clientX) / (uiScale || 1);
      const next = Math.round(Math.min(MAX, Math.max(MIN, startRef.current.w + delta)));
      setWidth(next);
    },
    [uiScale],
  );

  const onUp = useCallback(() => {
    setDragging(false);
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
    setWidth((w) => {
      api.setSidebarWidth(w).then(setPrefs).catch(() => {});
      return w;
    });
  }, [setPrefs]);

  useEffect(() => {
    if (!dragging) return;
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, [dragging, onMove, onUp]);

  const begin = (e: React.MouseEvent) => {
    e.preventDefault();
    startRef.current = { x: e.clientX, w: width };
    setDragging(true);
    // Held on the body so the cursor survives the pointer leaving the handle.
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  };

  // Keyboard resizing, so the column is reachable without a mouse.
  const onKey = (e: React.KeyboardEvent) => {
    const step = e.shiftKey ? 48 : 12;
    let next = width;
    if (e.key === 'ArrowLeft') next = Math.min(MAX, width + step);
    else if (e.key === 'ArrowRight') next = Math.max(MIN, width - step);
    else if (e.key === 'Home') next = 350;
    else return;
    e.preventDefault();
    setWidth(next);
    api.setSidebarWidth(next).then(setPrefs).catch(() => {});
  };

  // Below this the decorative bar column costs more than it gives: the name is
  // what the user is reading, and the percentage and size still carry the
  // proportion. Driven from the measured width rather than a container query,
  // which the WebView on early macOS 13 does not support.
  const tight = width < 430;

  return (
    <div className={'sidecol' + (tight ? ' sidecol-tight' : '')} style={{ width }}>
      <div
        className={'resizer' + (dragging ? ' resizer-active' : '')}
        onMouseDown={begin}
        onDoubleClick={() => {
          setWidth(350);
          api.setSidebarWidth(350).then(setPrefs).catch(() => {});
        }}
        onKeyDown={onKey}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the list. Arrow keys adjust, Home resets."
        aria-valuenow={width}
        aria-valuemin={MIN}
        aria-valuemax={MAX}
        tabIndex={0}
        title="Drag to resize · double-click to reset"
      />
      <div className="sidecol-body">{children}</div>
    </div>
  );
}
