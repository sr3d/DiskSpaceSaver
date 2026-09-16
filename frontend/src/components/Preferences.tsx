import { useEffect, useState } from 'react';
import { api, AppInfo, Prefs } from '../api';
import { PALETTES } from '../colors';
import { formatAge, formatBytes } from '../format';
import { reportError, useStore } from '../store';

export function Preferences() {
  const prefs = useStore((s) => s.prefs);
  const setPrefs = useStore((s) => s.setPrefs);
  const helper = useStore((s) => s.helper);
  const setHelper = useStore((s) => s.setHelper);
  const update = useStore((s) => s.update);
  const setUpdate = useStore((s) => s.setUpdate);
  const fda = useStore((s) => s.fda);
  const close = useStore((s) => s.setShowPrefs);
  const toast = useStore((s) => s.toast);

  const [info, setInfo] = useState<AppInfo | null>(null);
  const [cacheBytes, setCacheBytes] = useState(0);
  const [busy, setBusy] = useState('');

  useEffect(() => {
    api.getAppInfo().then(setInfo).catch(() => {});
    api.scanCacheBytes().then(setCacheBytes).catch(() => {});
  }, []);

  if (!prefs) return null;

  const patch = async (p: Partial<Prefs>) => {
    try {
      setPrefs(await api.setPrefs({ ...prefs, ...p }));
    } catch (err) {
      reportError('Could not save that preference', err);
    }
  };

  const addExclusion = async () => {
    try {
      const dir = await api.chooseFolder();
      if (!dir) return;
      setPrefs(await api.addExclusion(dir));
    } catch (err) {
      reportError('Could not add that folder', err);
    }
  };

  return (
    <div className="sheet-backdrop" onMouseDown={() => close(false)}>
      <div className="sheet" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-label="Preferences">
        <header className="sheet-head">
          <h2>Preferences</h2>
          <button className="icon-btn" onClick={() => close(false)} aria-label="Close">
            ×
          </button>
        </header>

        <div className="sheet-body">
          <section className="pref-group">
            <h3>Appearance</h3>
            <label className="pref-row">
              <span>Theme</span>
              <select value={prefs.theme} onChange={(e) => patch({ theme: e.target.value as Prefs['theme'] })}>
                <option value="system">Match system</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
              </select>
            </label>
            <label className="pref-row">
              <span>Colours</span>
              <select value={prefs.colorScheme} onChange={(e) => patch({ colorScheme: e.target.value })}>
                {PALETTES.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="pref-row">
              <span>Rings</span>
              <input
                type="range"
                min={3}
                max={9}
                value={prefs.rings}
                onChange={(e) => patch({ rings: Number(e.target.value) })}
              />
              <em>{prefs.rings}</em>
            </label>
            <label className="pref-row">
              <span>Sizes</span>
              <select value={prefs.base2 ? '2' : '10'} onChange={(e) => patch({ base2: e.target.value === '2' })}>
                <option value="10">Base 10 — GB, matches Finder</option>
                <option value="2">Base 2 — GiB</option>
              </select>
            </label>
            <label className="pref-check">
              <input type="checkbox" checked={prefs.showHidden} onChange={(e) => patch({ showHidden: e.target.checked })} />
              <span>Show hidden files in the list</span>
            </label>
            <p className="pref-hint">
              Hidden files are always scanned and always counted — they take up real space. This only changes whether
              they appear in the list.
            </p>
          </section>

          <section className="pref-group">
            <h3>Scanning</h3>
            <label className="pref-check">
              <input type="checkbox" checked={prefs.useCache} onChange={(e) => patch({ useCache: e.target.checked })} />
              <span>Remember the last scan of each volume</span>
            </label>
            <label className="pref-check">
              <input type="checkbox" checked={prefs.useBulkScan} onChange={(e) => patch({ useBulkScan: e.target.checked })} />
              <span>Use the fast scanning path</span>
            </label>
            <div className="pref-row">
              <span>Saved scans</span>
              <em>{formatBytes(cacheBytes, prefs.base2)}</em>
              <button
                className="btn btn-quiet"
                onClick={async () => {
                  await api.clearScanCache();
                  setCacheBytes(await api.scanCacheBytes());
                  toast('info', 'Saved scans cleared');
                }}
              >
                Clear
              </button>
            </div>

            <div className="pref-list">
              <div className="pref-list-head">
                <span>Never scan these folders</span>
                <button className="btn btn-quiet" onClick={addExclusion}>
                  Add…
                </button>
              </div>
              {prefs.exclusions.length === 0 ? (
                <p className="muted">No exclusions.</p>
              ) : (
                <ul>
                  {prefs.exclusions.map((x) => (
                    <li key={x}>
                      <code>{x}</code>
                      <button
                        className="icon-btn"
                        aria-label={`Stop excluding ${x}`}
                        onClick={async () => setPrefs(await api.removeExclusion(x))}
                      >
                        ×
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>

          <section className="pref-group">
            <h3>Deleting</h3>
            <label className="pref-check">
              <input type="checkbox" checked={prefs.confirmTrash} onChange={(e) => patch({ confirmTrash: e.target.checked })} />
              <span>Ask before moving items to the Trash</span>
            </label>
            <label className="pref-check">
              <input
                type="checkbox"
                checked={prefs.allowPermanentDelete}
                onChange={(e) => patch({ allowPermanentDelete: e.target.checked })}
              />
              <span>Allow permanent delete</span>
            </label>
            <p className="pref-hint">
              Permanent delete bypasses the Trash. Nothing can bring those files back, so it stays off until you ask
              for it.
            </p>
          </section>

          <section className="pref-group">
            <h3>Access</h3>
            <div className="pref-row">
              <span>Full Disk Access</span>
              <em className={fda?.granted ? 'ok' : 'warn'}>{fda?.granted ? 'granted' : 'not granted'}</em>
              {!fda?.granted && (
                <button className="btn btn-quiet" onClick={() => api.openFDASettings()}>
                  Open Settings…
                </button>
              )}
            </div>
            <p className="pref-hint">
              Without it, parts of your home folder and other users' files cannot be read and will show as unreadable.
            </p>

            <div className="pref-row">
              <span>Privileged helper</span>
              <em className={helper?.running && helper.compatible ? 'ok' : 'warn'}>
                {helper?.running ? (helper.compatible ? 'installed' : 'version mismatch') : helper?.registration ?? 'not installed'}
              </em>
              {helper?.needsApproval ? (
                <button className="btn btn-quiet" onClick={() => api.openLoginItemsSettings()}>
                  Approve…
                </button>
              ) : helper?.running ? (
                <button
                  className="btn btn-quiet"
                  disabled={busy === 'helper'}
                  onClick={async () => {
                    setBusy('helper');
                    try {
                      setHelper(await api.uninstallHelper());
                      toast('info', 'Helper removed');
                    } catch (e) {
                      reportError('Could not remove the helper', e);
                    } finally {
                      setBusy('');
                    }
                  }}
                >
                  Remove
                </button>
              ) : (
                <button
                  className="btn btn-quiet"
                  disabled={busy === 'helper' || !helper?.bundled}
                  onClick={async () => {
                    setBusy('helper');
                    try {
                      setHelper(await api.installHelper());
                      toast('info', 'Helper installed', 'Approve it in System Settings if prompted.');
                    } catch (e) {
                      reportError('Could not install the helper', e);
                    } finally {
                      setBusy('');
                    }
                  }}
                >
                  Install…
                </button>
              )}
            </div>
            {helper?.advice && <p className="pref-hint">{helper.advice}</p>}
          </section>

          <section className="pref-group">
            <h3>Updates</h3>
            <label className="pref-row">
              <span>Channel</span>
              <select
                value={prefs.updateChannel}
                onChange={(e) => patch({ updateChannel: e.target.value as Prefs['updateChannel'] })}
              >
                <option value="stable">Stable</option>
                <option value="beta">Beta</option>
              </select>
            </label>
            <label className="pref-check">
              <input
                type="checkbox"
                checked={prefs.autoCheckUpdates}
                onChange={(e) => patch({ autoCheckUpdates: e.target.checked })}
              />
              <span>Check for updates automatically</span>
            </label>
            <label className="pref-check">
              <input type="checkbox" checked={prefs.sendStats} onChange={(e) => patch({ sendStats: e.target.checked })} />
              <span>Send anonymous update statistics</span>
            </label>
            <p className="pref-hint">
              Version, macOS version and processor only, under a random identifier. No file names, no paths, nothing
              that identifies you.
            </p>
            <div className="pref-row">
              <span>Status</span>
              <em>
                {update?.checking
                  ? 'checking…'
                  : update?.updateAvailable
                    ? `${update.latestVersion} available`
                    : update?.err
                      ? update.err
                      : update?.supported
                        ? 'up to date'
                        : (update?.reason ?? 'unavailable')}
              </em>
              <button
                className="btn btn-quiet"
                disabled={!update?.supported}
                onClick={async () => setUpdate(await api.checkForUpdates())}
              >
                Check Now
              </button>
            </div>
          </section>

          <section className="pref-group">
            <h3>About</h3>
            <p className="muted">
              DiskSpaceSaver {info?.version} ({info?.build})
              {info && !info.bundled && ' — development build'}
              {info?.teamID && ` · team ${info.teamID}`}
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}
