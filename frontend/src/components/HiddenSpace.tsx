// Explains the gap between what a volume reports as used and what a scan can
// see: purgeable caches, APFS local snapshots, trees we were refused, and
// whatever remains.
import { useEffect, useState } from 'react';
import { api, HiddenSpaceReport } from '../api';
import { formatBytes, formatCount, formatSnapshotDate } from '../format';
import { reportError, useStore } from '../store';

interface Props {
  mount: string;
  base2: boolean;
}

export function HiddenSpace({ mount, base2 }: Props) {
  const close = useStore((s) => s.setHiddenSpaceMount);
  const toast = useStore((s) => s.toast);
  const askConfirm = useStore((s) => s.askConfirm);
  const [rep, setRep] = useState<HiddenSpaceReport | null>(null);
  const [err, setErr] = useState<string>('');
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  const reload = () => {
    api
      .hiddenSpace(mount)
      .then((r) => {
        setRep(r);
        setErr('');
      })
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)));
  };

  useEffect(reload, [mount]);

  const segments = rep
    ? [
        { key: 'visible', label: 'Files you can see', bytes: Math.max(rep.visible, 0), color: 'var(--seg-visible)' },
        { key: 'purge', label: 'Purgeable', bytes: rep.purgeable, color: 'var(--seg-purge)' },
        { key: 'snap', label: 'Snapshots', bytes: rep.snapshotBytes, color: 'var(--seg-snap)' },
        {
          key: 'other',
          label: rep.unaccounted >= 0 ? 'Unaccounted' : 'Shared by clones',
          bytes: Math.abs(rep.unaccounted),
          color: 'var(--seg-other)',
        },
        { key: 'free', label: 'Free', bytes: Math.max(rep.free, 0), color: 'var(--seg-free)' },
      ].filter((s) => s.bytes > 0)
    : [];
  const segTotal = segments.reduce((a, s) => a + s.bytes, 0) || 1;

  const deleteSnapshots = () => {
    const names = [...picked];
    if (names.length === 0) return;
    askConfirm({
      title: `Delete ${names.length} snapshot${names.length === 1 ? '' : 's'}?`,
      body:
        'Local Time Machine snapshots let you restore recent versions of files. macOS removes them on its own when space runs low.',
      confirmLabel: 'Delete Snapshots',
      destructive: true,
      onConfirm: async () => {
        setBusy(true);
        try {
          const res = await api.deleteLocalSnapshots(mount, names);
          if (res.succeeded?.length) toast('success', `${res.succeeded.length} snapshots deleted`);
          const failed = Object.keys(res.failed ?? {});
          if (failed.length) toast('error', `${failed.length} could not be deleted`, res.failed[failed[0]]);
          setPicked(new Set());
          reload();
        } catch (e) {
          reportError('Could not delete snapshots', e);
        } finally {
          setBusy(false);
        }
      },
    });
  };

  return (
    <div className="sheet-backdrop" onMouseDown={() => close(null)}>
      <div className="sheet sheet-wide" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-label="Hidden space">
        <header className="sheet-head">
          <h2>Hidden space on {rep?.volume ?? mount}</h2>
          <button className="icon-btn" onClick={() => close(null)} aria-label="Close">
            ×
          </button>
        </header>

        {err && <div className="notice notice-error">{err}</div>}

        {rep && (
          <div className="sheet-body">
            <div className="stack-bar" role="img" aria-label="How this volume's capacity is used">
              {segments.map((s) => (
                <div
                  key={s.key}
                  className="stack-seg"
                  style={{ width: `${(s.bytes / segTotal) * 100}%`, background: s.color }}
                  title={`${s.label} — ${formatBytes(s.bytes, base2)}`}
                />
              ))}
            </div>
            <div className="stack-legend">
              {segments.map((s) => (
                <div className="legend-item" key={s.key}>
                  <span className="legend-dot" style={{ background: s.color }} aria-hidden />
                  <span className="legend-label">{s.label}</span>
                  <span className="legend-value">{formatBytes(s.bytes, base2)}</span>
                </div>
              ))}
            </div>

            <dl className="facts">
              <div>
                <dt>Capacity</dt>
                <dd>{formatBytes(rep.total, base2)}</dd>
              </div>
              <div>
                <dt>Used</dt>
                <dd>{formatBytes(rep.used, base2)}</dd>
              </div>
              <div>
                <dt>Scanned</dt>
                <dd>{rep.hasScan ? formatBytes(rep.visible, base2) : 'not scanned'}</dd>
              </div>
              <div>
                <dt>Unreadable items</dt>
                <dd>{formatCount(rep.inaccessible)}</dd>
              </div>
            </dl>

            {rep.notes.map((n, i) => (
              <p className="note" key={i}>
                {n}
              </p>
            ))}

            <section className="snapshots">
              <header>
                <h3>Local snapshots ({rep.snapshots.length})</h3>
                {rep.snapshots.length > 0 && (
                  <button className="btn btn-danger" disabled={picked.size === 0 || busy} onClick={deleteSnapshots}>
                    Delete {picked.size || ''} selected
                  </button>
                )}
              </header>
              {rep.snapshots.length === 0 ? (
                <p className="muted">No local Time Machine snapshots on this volume.</p>
              ) : (
                <ul>
                  {rep.snapshots.map((s) => (
                    <li key={s.name}>
                      <label>
                        <input
                          type="checkbox"
                          checked={picked.has(s.name)}
                          onChange={(e) => {
                            const next = new Set(picked);
                            e.target.checked ? next.add(s.name) : next.delete(s.name);
                            setPicked(next);
                          }}
                        />
                        <span className="snap-date">{formatSnapshotDate(s.date)}</span>
                        <span className="snap-name">{s.name}</span>
                      </label>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            <footer className="sheet-foot">
              <button className="btn btn-quiet" onClick={() => api.openStorageSettings()}>
                Open Storage Settings
              </button>
              <button className="btn" onClick={reload}>
                Refresh
              </button>
            </footer>
          </div>
        )}
      </div>
    </div>
  );
}
