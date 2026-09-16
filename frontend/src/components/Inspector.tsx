// The details of whatever is selected, in its own column to the right of the
// list. QuickLook renders the preview, so images, PDFs, video and documents
// all arrive as a picture without this component knowing any file format.
import { useEffect, useRef, useState } from 'react';
import {
  api, ItemRef, KIND_DIR, KIND_PACKAGE, FLAG_CLOUD, FLAG_DUP, FLAG_NOPERM,
  Preview as PreviewData,
} from '../api';
import { formatBytes, formatCount } from '../format';

interface Props {
  scanID: string;
  nodeID: number;
  base2: boolean;
  files: number;
  inline: boolean;
  flags: number;
  onQuickLook(): void;
  onReveal(): void;
}

export function Inspector(props: Props) {
  const { scanID, nodeID, base2, files, inline, flags, onQuickLook, onReveal } = props;
  const [data, setData] = useState<PreviewData | null>(null);
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    if (!nodeID) {
      setData(null);
      return;
    }
    const mine = ++seq.current;
    setLoading(true);
    const ref: ItemRef = { scanID, nodeID };
    api
      .previewItem(ref, inline ? 200 : 460)
      .then((p) => {
        // Arrowing down a list starts a render per row; only the last one
        // asked for is still wanted.
        if (mine === seq.current) setData(p);
      })
      .catch(() => {
        if (mine === seq.current) setData(null);
      })
      .finally(() => {
        if (mine === seq.current) setLoading(false);
      });
  }, [scanID, nodeID, inline]);

  if (!nodeID) {
    return inline ? null : (
      <aside className="inspector inspector-empty" aria-label="Item details">
        <p>Nothing selected</p>
        <span>Click an arc or a row to see what it is.</span>
      </aside>
    );
  }
  if (!data) return null;

  const isDir = data.kind === KIND_DIR || data.kind === KIND_PACKAGE;
  return (
    <aside className={'inspector' + (inline ? ' inspector-inline' : '')} aria-label="Item details">
      {data.image ? (
        <button
          className="inspector-image"
          onClick={onQuickLook}
          title="Open in Quick Look (Space)"
          aria-label={`Preview of ${data.name}. Open in Quick Look.`}
        >
          <img src={data.image} alt="" />
        </button>
      ) : (
        <div className={'inspector-blank' + (loading ? ' inspector-loading' : '')}>
          <span>{isDir ? 'folder' : data.ext || 'file'}</span>
        </div>
      )}

      <div className="inspector-body">
        <div className="inspector-name" title={data.name}>
          {data.name}
        </div>
        <div className="inspector-size">{formatBytes(data.size, base2)}</div>

        <dl className="inspector-facts">
          {isDir && (
            <div>
              <dt>Contains</dt>
              <dd>{formatCount(files)} files</dd>
            </div>
          )}
          {data.mtime > 0 && (
            <div>
              <dt>Modified</dt>
              <dd>{new Date(data.mtime * 1000).toLocaleDateString()}</dd>
            </div>
          )}
          <div>
            <dt>Kind</dt>
            <dd>{isDir ? 'Folder' : (data.ext || 'File').toUpperCase()}</dd>
          </div>
        </dl>

        {(flags & FLAG_NOPERM) !== 0 && (
          <p className="inspector-note">Could not be read. Grant Full Disk Access or scan as administrator.</p>
        )}
        {(flags & FLAG_CLOUD) !== 0 && (
          <p className="inspector-note">Cloud storage — kept online, not using space on this disk.</p>
        )}
        {(flags & FLAG_DUP) !== 0 && (
          <p className="inspector-note">A hardlink. Its bytes are counted once, at the first copy found.</p>
        )}

        <button className="inspector-path" onClick={onReveal} title="Show in Finder">
          {data.path}
        </button>
      </div>
    </aside>
  );
}
