// The details of whatever is selected, in its own column to the right of the
// list. QuickLook renders the preview, so images, PDFs, video and documents
// all arrive as a picture without this component knowing any file format.
import { useEffect, useRef, useState } from 'react';
import {
  api, ItemRef, KIND_DIR, KIND_PACKAGE, FLAG_CLOUD, FLAG_DUP, FLAG_NOPERM,
  Preview as PreviewData,
} from '../api';
import { formatBytes, formatCount } from '../format';
import { useStore } from '../store';

interface Props {
  scanID: string;
  nodeID: number;
  base2: boolean;
  files: number;
  inline: boolean;
  flags: number;
  onQuickLook(): void;
  onReveal(): void;
  onCopyPath(): void;
}

export function Inspector(props: Props) {
  const { scanID, nodeID, base2, files, inline, flags, onQuickLook, onReveal, onCopyPath } = props;
  const caps = useStore((s) => s.caps);
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
      {/* Only a real preview earns the space. What used to sit here when
          there was none was a square the size of the panel's top third
          containing the word FOLDER — which the Kind row below already says,
          in a place the eye can scan. */}
      {data.image && (
        <button
          className={'inspector-image' + (loading ? ' inspector-loading' : '')}
          onClick={onQuickLook}
          title={caps.preview ? `Open in Quick Look (Space)` : `Open (Space)`}
          aria-label={`Preview of ${data.name}. ${caps.preview ? "Open in Quick Look." : "Open."}`}
        >
          <img src={data.image} alt="" />
        </button>
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
          <p className="inspector-note">{caps.fullDiskAccess
              ? 'Could not be read. Grant Full Disk Access or scan as administrator.'
              : 'Could not be read. Scan as administrator to include it.'}</p>
        )}
        {(flags & FLAG_CLOUD) !== 0 && (
          <p className="inspector-note">Cloud storage — kept online, not using space on this disk.</p>
        )}
        {(flags & FLAG_DUP) !== 0 && (
          <p className="inspector-note">A hardlink. Its bytes are counted once, at the first copy found.</p>
        )}

        {/* Where the thing is, which is most of what someone opens this
            panel to find out, so it is legible rather than a grey footnote.
            Clicking it still reveals in the file manager; the button beside it
            copies, which is the other thing a path is for. */}
        <div className="inspector-where">
          <button
            className="inspector-path"
            onClick={onReveal}
            title={`${caps.revealLabel}`}
          >
            {data.path}
          </button>
          <button
            className="icon-btn inspector-copy"
            onClick={onCopyPath}
            title={`Copy path (${caps.modKey}\u21e7C)`}
            aria-label="Copy the path"
          >
            {/* Drawn rather than set in type: the glyphs for this idea render
                differently or not at all depending on the font stack. */}
            <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden focusable="false">
              <rect x="5.2" y="5.2" width="8.4" height="8.4" rx="2"
                fill="none" stroke="currentColor" strokeWidth="1.4" />
              <path d="M10.8 5.2V4.4a2 2 0 0 0-2-2H4.4a2 2 0 0 0-2 2v4.4a2 2 0 0 0 2 2h0.8"
                fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
            </svg>
          </button>
        </div>
      </div>
    </aside>
  );
}
