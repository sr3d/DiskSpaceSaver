// The ticked rows, gathered into a pane of their own.
//
// The summary and the buttons used to sit in a bar above the list, which meant
// the list jumped down the moment a box was ticked — the one time the user is
// least able to afford the rows moving, because they are in the middle of
// picking several of them. So the count lives in the footer instead, and this
// pane floats over the list rather than taking space from it.
import { KIND_DIR, KIND_PACKAGE, NodeSummary } from '../api';
import { arcColor, Palette } from '../colors';
import { formatBytes, formatCount } from '../format';
import { useStore } from '../store';

interface Props {
  rows: NodeSummary[];
  /** Index of each row in the list, so the swatch matches the chart. */
  indexOf(id: number): number;
  bytes: number;
  /** The folder the ticks are in — every one of them is a child of it. */
  where: string;
  base2: boolean;
  palette: Palette;
  dark: boolean;
  allowPermanent: boolean;
  hovered: number;
  onHover(id: number): void;
  onSelect(id: number): void;
  onContextMenu(id: number, x: number, y: number): void;
  onRemove(id: number): void;
  onCollect(): void;
  onTrash(): void;
  onDeleteForever(): void;
  onClear(): void;
  onClose(): void;
}

export function SelectionPane(props: Props) {
  const mod = useStore((s) => s.caps.modKey);
  const {
    rows, indexOf, bytes, where, base2, palette, dark, allowPermanent, hovered,
    onHover, onSelect, onContextMenu, onRemove, onCollect, onTrash,
    onDeleteForever, onClear, onClose,
  } = props;

  return (
    <aside className="selpane" aria-label="Selected items">
      <header className="collector-head">
        <h2 className="selpane-title" title={where}>
          Selected<span> in {where}</span>
        </h2>
        <button className="icon-btn" onClick={onClose} aria-label="Close the selection list">
          ×
        </button>
      </header>

      <div className="collector-total">
        <strong>{formatBytes(bytes, base2)}</strong>
        <span>
          in {rows.length} item{rows.length === 1 ? '' : 's'}
        </span>
      </div>

      <div className="collector-list">
        {rows.map((r) => {
          const isDir = r.kind === KIND_DIR || r.kind === KIND_PACKAGE;
          const style = arcColor(palette, indexOf(r.id), 0, r.kind, r.flags, false, dark);
          return (
            <div
              className={'collected selected-item' + (r.id === hovered ? ' selected-item-hovered' : '')}
              key={r.id}
              onMouseEnter={() => onHover(r.id)}
              onMouseLeave={() => onHover(0)}
              onClick={() => onSelect(r.id)}
              onContextMenu={(e) => {
                e.preventDefault();
                onSelect(r.id);
                onContextMenu(r.id, e.clientX, e.clientY);
              }}
            >
              <span className="row-swatch selected-swatch" style={{ background: style.fill }} aria-hidden />
              <div className="collected-main">
                <div className="collected-name" title={r.name}>
                  {r.name}
                </div>
                <div className="collected-path">
                  {isDir ? `folder · ${formatCount(r.files)} files` : extOf(r.name)}
                </div>
              </div>
              <div className="collected-size">{formatBytes(r.size, base2)}</div>
              <button
                className="icon-btn"
                aria-label={`Remove ${r.name} from the selection`}
                title="Remove from the selection"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemove(r.id);
                }}
              >
                ×
              </button>
            </div>
          );
        })}
      </div>

      <footer className="collector-foot">
        <button className="btn btn-quiet" onClick={onClear} title="Untick everything">
          Clear
        </button>
        <button className="btn btn-quiet" onClick={onCollect} title={`Add these to the collector (${mod}D)`}>
          Collect
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

// NodeSummary carries no path or extension — the list only ever needed the
// name — so the kind line is derived from the name here.
function extOf(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return 'file';
  return name.slice(dot + 1).toUpperCase();
}
