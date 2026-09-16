// The list beside the chart: children of the focus, largest first, with a bar
// for each. Hover is synchronised with the chart in both directions.
import { useEffect, useRef } from 'react';
import { NodeSummary, KIND_DIR, KIND_PACKAGE, FLAG_NOPERM, FLAG_HIDDEN, FLAG_DUP, FLAG_CLOUD } from '../api';
import { formatBytes, formatCount, formatPercent } from '../format';
import { arcColor, Palette } from '../colors';

interface Props {
  rows: NodeSummary[];
  checked: Set<number>;
  onToggleCheck(id: number, shiftKey: boolean): void;
  total: number;
  palette: Palette;
  dark: boolean;
  base2: boolean;
  hovered: number;
  selected: number;
  collected: Set<number>;
  loading: boolean;
  onHover(id: number): void;
  onSelect(id: number): void;
  onOpen(id: number): void;
  onContextMenu(id: number, x: number, y: number): void;
}

export function Sidebar(props: Props) {
  const {
    rows, total, palette, dark, base2, hovered, selected, collected, loading,
    checked, onToggleCheck, onHover, onSelect, onOpen, onContextMenu,
  } = props;
  const listRef = useRef<HTMLDivElement | null>(null);

  // Keep the selected row visible when selection moves from the chart or the
  // keyboard rather than from a click in this list.
  useEffect(() => {
    if (!selected || !listRef.current) return;
    const el = listRef.current.querySelector<HTMLElement>(`[data-id="${selected}"]`);
    el?.scrollIntoView({ block: 'nearest' });
  }, [selected]);

  if (!loading && rows.length === 0) {
    return (
      <div className="sidebar">
        <div className="sidebar-empty">
          <p>Nothing inside</p>
          <span>This folder is empty, or everything in it was filtered out.</span>
        </div>
      </div>
    );
  }

  return (
    <div className="sidebar" ref={listRef} role="listbox" aria-label="Contents, largest first">
      {rows.map((r, i) => {
        const isDir = r.kind === KIND_DIR || r.kind === KIND_PACKAGE;
        const style = arcColor(palette, i, 0, r.kind, r.flags, false, dark);
        const share = total > 0 ? r.size / total : 0;
        return (
          <div
            key={r.id}
            data-id={r.id}
            role="option"
            aria-selected={r.id === selected}
            tabIndex={-1}
            className={
              'row' +
              (r.id === selected ? ' row-selected' : '') +
              (r.id === hovered ? ' row-hovered' : '') +
              (r.flags & FLAG_HIDDEN ? ' row-hidden-file' : '')
            }
            onMouseEnter={() => onHover(r.id)}
            onMouseLeave={() => onHover(0)}
            onClick={() => onSelect(r.id)}
            onDoubleClick={() => isDir && r.hasKids && onOpen(r.id)}
            onContextMenu={(e) => {
              e.preventDefault();
              onSelect(r.id);
              onContextMenu(r.id, e.clientX, e.clientY);
            }}
            title={`${r.name} — ${formatBytes(r.size, base2)}`}
          >
            <input
              type="checkbox"
              className="row-check"
              checked={checked.has(r.id)}
              // The row underneath selects on click; ticking a box is a
              // different intent and must not also move the selection.
              onClick={(e) => e.stopPropagation()}
              onChange={(e) => {
                const native = e.nativeEvent as MouseEvent;
                onToggleCheck(r.id, native.shiftKey);
              }}
              aria-label={`Select ${r.name}`}
            />
            <span className="row-swatch" style={{ background: style.fill }} aria-hidden />
            <span className="row-name">
              {r.name}
              {collected.has(r.id) && <span className="row-badge" title="In the collector">●</span>}
              {(r.flags & FLAG_NOPERM) !== 0 && (
                <span className="row-warn" title="Could not be read">!</span>
              )}
              {(r.flags & FLAG_DUP) !== 0 && (
                <span className="row-warn" title="Hardlink — its space is counted elsewhere">⇄</span>
              )}
              {(r.flags & FLAG_CLOUD) !== 0 && (
                <span className="row-cloud" title="Cloud storage — kept online, not on this disk">☁</span>
              )}
            </span>
            <span className="row-bar" aria-hidden>
              <span className="row-bar-fill" style={{ width: `${Math.max(share * 100, 0.6)}%`, background: style.fill }} />
            </span>
            <span className="row-share">{formatPercent(share)}</span>
            <span className="row-size">{formatBytes(r.size, base2)}</span>
            <span className="row-meta">
              {(r.flags & FLAG_CLOUD) !== 0
                ? 'stored online — not using disk space here'
                : isDir
                  ? `${formatCount(r.files)} files`
                  : ''}
            </span>
          </div>
        );
      })}
      {loading && <div className="sidebar-loading">loading…</div>}
    </div>
  );
}
