// A look at whatever is selected. QuickLook renders it, so images, PDFs,
// video and documents all arrive as a picture without this component knowing
// any file format; anything the system cannot represent falls back to the
// details, which are still worth showing.
import { useEffect, useRef, useState } from 'react';
import { api, ItemRef, KIND_DIR, KIND_PACKAGE, Preview as PreviewData } from '../api';
import { formatBytes, middleTruncate } from '../format';

interface Props {
  scanID: string;
  nodeID: number;
  base2: boolean;
  onQuickLook(): void;
}

export function Preview({ scanID, nodeID, base2, onQuickLook }: Props) {
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
      .previewItem(ref, 320)
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
  }, [scanID, nodeID]);

  if (!nodeID || !data) return null;
  const isDir = data.kind === KIND_DIR || data.kind === KIND_PACKAGE;

  return (
    <div className="preview">
      {data.image ? (
        <button
          className="preview-image"
          onClick={onQuickLook}
          title="Open in Quick Look (Space)"
          aria-label={`Preview of ${data.name}. Open in Quick Look.`}
        >
          <img src={data.image} alt="" />
        </button>
      ) : (
        <div className={'preview-blank' + (loading ? ' preview-loading' : '')}>
          <span className="preview-ext">{isDir ? 'folder' : data.ext || 'file'}</span>
        </div>
      )}
      <div className="preview-meta">
        <div className="preview-name" title={data.name}>
          {data.name}
        </div>
        <div className="preview-sub">
          {formatBytes(data.size, base2)}
          {data.mtime > 0 && ` · ${new Date(data.mtime * 1000).toLocaleDateString()}`}
        </div>
        <div className="preview-path" title={data.path}>
          {middleTruncate(data.path, 46)}
        </div>
      </div>
    </div>
  );
}
