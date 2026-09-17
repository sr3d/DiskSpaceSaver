// The opening screen: every mounted volume with its capacity bar.
import { useEffect, useState } from 'react';
import { api, CachedScan, Volume } from '../api';
import { formatAge, formatBytes } from '../format';

interface Props {
  volumes: Volume[];
  base2: boolean;
  helperAvailable: boolean;
  // These return promises so the card can stay busy until the work is done:
  // opening a large saved scan takes seconds of decoding before any tab
  // appears, and without feedback it reads as a dead button.
  onScan(path: string, asAdmin: boolean): Promise<void> | void;
  onOpenCached(path: string): Promise<void> | void;
  onChooseFolder(): void;
  onHiddenSpace(mount: string): void;
}

export function VolumePicker(props: Props) {
  const { volumes, base2, helperAvailable, onScan, onOpenCached, onChooseFolder, onHiddenSpace } = props;
  const [cached, setCached] = useState<Record<string, CachedScan | null>>({});
  // Which card is working, and at what. Keyed by path so one busy volume does
  // not disable the others.
  const [busy, setBusy] = useState<Record<string, string>>({});

  const run = async (path: string, what: string, fn: () => Promise<void> | void) => {
    if (busy[path]) return; // a second click while the first is still running
    setBusy((b) => ({ ...b, [path]: what }));
    try {
      await fn();
    } finally {
      setBusy((b) => {
        const next = { ...b };
        delete next[path];
        return next;
      });
    }
  };

  // Ask which volumes have a saved scan so each card can offer to reopen it.
  useEffect(() => {
    let live = true;
    (async () => {
      const out: Record<string, CachedScan | null> = {};
      for (const v of volumes) {
        try {
          const c = await api.lookupCachedScan(v.path);
          out[v.path] = c?.found ? c : null;
        } catch {
          out[v.path] = null;
        }
      }
      if (live) setCached(out);
    })();
    return () => {
      live = false;
    };
  }, [volumes]);

  return (
    <div className="volumes" style={{ '--wails-drop-target': 'drop' } as any}>
      <header className="volumes-head">
        <h1>Where has your disk gone?</h1>
        <p>Pick a volume to scan, or drop a folder anywhere on this window.</p>
      </header>

      <div className="volume-grid">
        {volumes.map((v) => {
          const usedFrac = v.total > 0 ? v.used / v.total : 0;
          const purgeFrac = v.total > 0 ? v.purgeable / v.total : 0;
          const c = cached[v.path];
          return (
            <div key={v.path + v.uuid} className="volume-card">
              <div className="volume-top">
                {v.icon ? (
                  <img className="volume-icon" src={v.icon} alt="" />
                ) : (
                  <div className="volume-icon volume-icon-fallback" aria-hidden />
                )}
                <div className="volume-id">
                  <div className="volume-name" title={v.path}>
                    {v.name}
                  </div>
                  <div className="volume-sub">
                    {v.fs}
                    {v.network ? ' · network' : v.removable ? ' · removable' : v.internal ? ' · internal' : ''}
                    {v.readOnly ? ' · read-only' : ''}
                  </div>
                </div>
              </div>

              <div className="capacity" role="img"
                   aria-label={`${formatBytes(v.used, base2)} used of ${formatBytes(v.total, base2)}`}>
                <div className="capacity-used" style={{ width: `${usedFrac * 100}%` }} />
                {purgeFrac > 0.002 && (
                  <div
                    className="capacity-purge"
                    style={{ width: `${purgeFrac * 100}%`, left: `${Math.max(usedFrac * 100 - purgeFrac * 100, 0)}%` }}
                    title={`${formatBytes(v.purgeable, base2)} purgeable`}
                  />
                )}
              </div>
              <div className="capacity-text">
                <strong>{formatBytes(v.free, base2)}</strong> free of {formatBytes(v.total, base2)}
                {v.purgeable > 0 && (
                  <span className="capacity-purge-text"> · {formatBytes(v.purgeable, base2)} purgeable</span>
                )}
              </div>

              {c && (
                <button
                  className="volume-cached"
                  disabled={!!busy[v.path]}
                  onClick={() => run(v.path, 'opening', () => onOpenCached(v.path))}
                >
                  {busy[v.path] === 'opening' ? (
                    <>
                      <span className="spinner" aria-hidden /> Opening saved scan…
                    </>
                  ) : (
                    <>Open last scan — {formatAge(c.ageMillis)}</>
                  )}
                </button>
              )}

              <div className="volume-actions">
                <button
                  className="btn btn-primary"
                  onClick={() => run(v.path, 'scanning', () => onScan(v.path, false))}
                  disabled={!!busy[v.path] || (v.readOnly && v.network)}
                >
                  {busy[v.path] === 'scanning' ? (
                    <>
                      <span className="spinner" aria-hidden /> Starting…
                    </>
                  ) : (
                    'Scan'
                  )}
                </button>
                <button
                  className="btn"
                  onClick={() => run(v.path, 'scanning', () => onScan(v.path, true))}
                  disabled={!helperAvailable || !!busy[v.path]}
                  title={
                    helperAvailable
                      ? 'Scan with the privileged helper to include other users’ files'
                      : 'Install the privileged helper in Preferences to scan as administrator'
                  }
                >
                  As Admin
                </button>
                <button className="btn btn-quiet" onClick={() => onHiddenSpace(v.path)}>
                  Hidden space
                </button>
              </div>
            </div>
          );
        })}

        <button className="volume-card volume-card-add" onClick={onChooseFolder}>
          <div className="add-plus" aria-hidden>+</div>
          <div className="add-label">Scan a folder…</div>
          <div className="add-sub">or drop one onto this window</div>
        </button>
      </div>
    </div>
  );
}
