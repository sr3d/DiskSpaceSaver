// The opening screen: every mounted volume with its capacity bar.
import { useEffect, useState } from 'react';
import { api, CachedScan, Volume } from '../api';
import { formatAge, formatBytes } from '../format';

interface Props {
  volumes: Volume[];
  base2: boolean;
  helperAvailable: boolean;
  onScan(path: string, asAdmin: boolean): void;
  onOpenCached(path: string): void;
  onChooseFolder(): void;
  onHiddenSpace(mount: string): void;
}

export function VolumePicker(props: Props) {
  const { volumes, base2, helperAvailable, onScan, onOpenCached, onChooseFolder, onHiddenSpace } = props;
  const [cached, setCached] = useState<Record<string, CachedScan | null>>({});

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
                <button className="volume-cached" onClick={() => onOpenCached(v.path)}>
                  Open last scan — {formatAge(c.ageMillis)}
                </button>
              )}

              <div className="volume-actions">
                <button className="btn btn-primary" onClick={() => onScan(v.path, false)} disabled={v.readOnly && v.network}>
                  Scan
                </button>
                <button
                  className="btn"
                  onClick={() => onScan(v.path, true)}
                  disabled={!helperAvailable}
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
