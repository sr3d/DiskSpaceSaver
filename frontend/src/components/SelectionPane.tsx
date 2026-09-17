// The ticked rows, gathered into a column of their own.
//
// The summary and the buttons used to sit in a bar above the list, which meant
// the list jumped down the moment a box was ticked — the one time the user is
// least able to afford the rows moving, because they are in the middle of
// picking several of them. So the count lives in the footer instead.
//
// The selection now spans folders, which is why each row carries its full
// path: ticks survive navigation, so "everything ticked is on screen" no
// longer holds, and this list is what makes a bulk delete reviewable.
import { KIND_DIR, KIND_PACKAGE, SelectedItem } from '../api';
import { arcColor, Palette } from '../colors';
import { formatBytes, formatCount, middleTruncate } from '../format';
import { useStore } from '../store';

interface Props {
  items: SelectedItem[];
  /** Index of each row in the current list, so the swatch matches the chart. */
  indexOf(id: number): number;
  bytes: number;
  /** How many distinct folders the selection spans. */
  folders: number;
  base2: boolean;
  palette: Palette;
  dark: boolean;
  allowPermanent: boolean;
  hovered: number;
  onHover(id: number): void;
  onSelect(item: SelectedItem): void;
  onContextMenu(id: number, x: number, y: number): void;
  onRemove(id: number): void;
  onWatch(): void;
  onTrash(): void;
  onDeleteForever(): void;
  onClear(): void;
  onClose(): void;
}

export function SelectionPane(props: Props) {
  const mod = useStore((s) => s.caps.modKey);
  const {
    items, indexOf, bytes, folders, base2, palette, dark, allowPermanent, hovered,
    onHover, onSelect, onContextMenu, onRemove, onWatch, onTrash,
    onDeleteForever, onClear, onClose,
  } = props;

  const live = items.filter((i) => !i.gone);

  return (
    <aside className="selpane" aria-label="Selected items">
      <header className="panel-head">
        <h2 className="selpane-title">
          Selected
          {folders > 1 && <span> across {folders} folders</span>}
        </h2>
        <button className="icon-btn" onClick={onClose} aria-label="Close the selection list">
          ×
        </button>
      </header>

      <div className="panel-total">
        <strong>{formatBytes(bytes, base2)}</strong>
        <span>
          in {live.length} item{live.length === 1 ? '' : 's'}
        </span>
      </div>

      <div className="panel-list">
        {items.map((it) => {
          const isDir = it.kind === KIND_DIR || it.kind === KIND_PACKAGE;
          // An item from another folder has no place in the current list, so
          // its index there means nothing; a stable hue from its path keeps
          // the swatch from jumping about as the user walks the tree.
          const idx = indexOf(it.nodeID);
          const style = arcColor(palette, idx >= 0 ? idx : hueFor(it.path), 0, it.kind, it.flags, false, dark);
          return (
            <div
              className={
                'panel-item selected-item' +
                (it.nodeID === hovered ? ' selected-item-hovered' : '') +
                (it.gone ? ' selected-item-gone' : '') +
                (it.covered ? ' selected-item-covered' : '')
              }
              key={`${it.scanID}:${it.nodeID}`}
              onMouseEnter={() => onHover(it.nodeID)}
              onMouseLeave={() => onHover(0)}
              onClick={() => onSelect(it)}
              onContextMenu={(e) => {
                e.preventDefault();
                onSelect(it);
                onContextMenu(it.nodeID, e.clientX, e.clientY);
              }}
            >
              <span className="row-swatch selected-swatch" style={{ background: style.fill }} aria-hidden />
              <div className="panel-item-main">
                <div className="panel-item-name" title={it.path || it.name}>
                  {it.name || 'item'}
                </div>
                <div className="panel-item-path" title={it.path}>
                  {it.gone
                    ? it.err || 'no longer there'
                    : it.covered
                      ? 'inside another selected folder'
                      : whereOf(it.path, isDir ? `folder · ${formatCount(it.files)} files` : extOf(it.name))}
                </div>
              </div>
              {/* A covered item contributes nothing to the total, so showing
                  its size beside a total that excludes it invites the user to
                  add the column up and find it wrong. */}
              <div className="panel-item-size">
                {it.gone ? '—' : it.covered ? '' : formatBytes(it.size, base2)}
              </div>
              <button
                className="icon-btn"
                aria-label={`Remove ${it.name || 'this item'} from the selection`}
                title="Remove from the selection"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemove(it.nodeID);
                }}
              >
                ×
              </button>
            </div>
          );
        })}
      </div>

      <footer className="panel-foot">
        <button className="btn btn-quiet" onClick={onClear} title="Untick everything">
          Clear
        </button>
        <button className="btn btn-quiet" onClick={onWatch} title={`Watch these in the Monitor (${mod}D)`}>
          Watch
        </button>
        <button className="btn btn-danger" onClick={onTrash}>
          Move to Trash
        </button>
        {allowPermanent && (
          <button className="btn btn-danger-outline" onClick={onDeleteForever}>
            Delete…
          </button>
        )}
      </footer>
    </aside>
  );
}

// The folder an item is in, which is the thing the user cannot tell from the
// name once the selection spans several of them. Falls back to the kind line
// when there is no path to show.
function whereOf(path: string, fallback: string): string {
  const cut = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  if (cut <= 0) return fallback;
  return middleTruncate(path.slice(0, cut), 44);
}

// SelectedItem carries no extension — the name is all the list ever needed —
// so the kind line is derived from the name here.
function extOf(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return 'file';
  return name.slice(dot + 1).toUpperCase();
}

// A small stable hash, so an item keeps the same swatch for as long as it is
// ticked no matter which folder the user happens to be looking at.
function hueFor(path: string): number {
  let h = 0;
  for (let i = 0; i < path.length; i++) h = (h * 31 + path.charCodeAt(i)) | 0;
  return Math.abs(h);
}
