// A side column with a drag handle on its leading edge.
//
// Extracted from the list column when the selection pane became a column too.
// It is one component rather than two because the one thing that went wrong
// the first time — forgetting that pointer deltas are in visual pixels while
// widths are layout values, so at 150% interface scale the handle ran away
// from the cursor — is exactly the thing a second copy would get wrong again.
//
// The width is owned by the caller and persisted as a preference, but written
// only when the drag ends: persisting on every mouse move is a file write per
// pixel.
import { ReactNode, useCallback, useEffect, useRef, useState } from 'react';

interface Props {
  /** The stored width. Followed while not dragging, so another window's change lands. */
  width: number;
  min: number;
  max: number;
  /** Width restored by double-clicking the handle or pressing Home. */
  fallback: number;
  uiScale: number;
  /** Called once, when the drag ends or a key changes the width. */
  onCommit(width: number): void;
  /** Base class for the column; `<className>-tight` is added below tightBelow. */
  className: string;
  /** Width under which the column is too narrow for its full layout. */
  tightBelow?: number;
  /** What the handle resizes, for screen readers: "the list", "the selection". */
  label: string;
  children: ReactNode;
}

export function ResizableColumn(props: Props) {
  const { width: stored, min, max, fallback, uiScale, onCommit, className, tightBelow, label, children } = props;
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
      setWidth(Math.round(Math.min(max, Math.max(min, startRef.current.w + delta))));
    },
    [uiScale, min, max],
  );

  const onUp = useCallback(() => {
    setDragging(false);
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
    setWidth((w) => {
      onCommit(w);
      return w;
    });
  }, [onCommit]);

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
    if (e.key === 'ArrowLeft') next = Math.min(max, width + step);
    else if (e.key === 'ArrowRight') next = Math.max(min, width - step);
    else if (e.key === 'Home') next = fallback;
    else return;
    e.preventDefault();
    setWidth(next);
    onCommit(next);
  };

  const tight = tightBelow !== undefined && width < tightBelow;

  return (
    <div className={className + (tight ? ` ${className}-tight` : '')} style={{ width }}>
      <div
        className={'resizer' + (dragging ? ' resizer-active' : '')}
        onMouseDown={begin}
        onDoubleClick={() => {
          setWidth(fallback);
          onCommit(fallback);
        }}
        onKeyDown={onKey}
        role="separator"
        aria-orientation="vertical"
        aria-label={`Resize ${label}. Arrow keys adjust, Home resets.`}
        aria-valuenow={width}
        aria-valuemin={min}
        aria-valuemax={max}
        tabIndex={0}
        title="Drag to resize · double-click to reset"
      />
      <div className="sidecol-body">{children}</div>
    </div>
  );
}
