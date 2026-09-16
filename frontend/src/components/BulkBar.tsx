// Appears once rows are ticked. Deliberately spells out how many items and how
// much space, because this is the one control in the app that removes several
// things at once.
import { formatBytes, formatCount } from '../format';

interface Props {
  count: number;
  bytes: number;
  base2: boolean;
  allowPermanent: boolean;
  onCollect(): void;
  onTrash(): void;
  onDeleteForever(): void;
  onClear(): void;
}

export function BulkBar(props: Props) {
  const { count, bytes, base2, allowPermanent, onCollect, onTrash, onDeleteForever, onClear } = props;
  if (count === 0) return null;
  return (
    <div className="bulkbar" role="toolbar" aria-label="Actions for the selected items">
      <span className="bulkbar-count">
        <strong>{formatCount(count)}</strong> selected · {formatBytes(bytes, base2)}
      </span>
      <button className="btn btn-quiet" onClick={onCollect} title="Add these to the collector (⌘D)">
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
      <button className="icon-btn" onClick={onClear} aria-label="Clear the selection" title="Clear">
        ×
      </button>
    </div>
  );
}
