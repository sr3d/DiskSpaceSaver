// The watchlist. Folders gathered from any scan and kept in view, with what
// each weighed when it was first watched beside what it weighs now.
//
// Deliberately not the same thing as the ticked selection, which is a working
// set assembled for one action and emptied straight after. These sit here
// across scans, so the number that earns the space is the change, not the
// size — and the destructive actions are behind a menu rather than under the
// user's thumb, because this is a panel you leave open.
import { useCallback, useState } from 'react';
import { api } from '../api';
import { formatBytes, middleTruncate } from '../format';
import { reportError, useStore } from '../store';
import { ContextMenu, MenuItem } from './ContextMenu';
import { ResizableColumn } from './ResizableColumn';

const MIN = 220;
const MAX = 900;
const DEFAULT = 330;

interface Props {
  base2: boolean;
}

export function Monitor({ base2 }: Props) {
  const monitor = useStore((s) => s.monitor);
  const close = useStore((s) => s.setShowMonitor);
  const askConfirm = useStore((s) => s.askConfirm);
  const toast = useStore((s) => s.toast);
  const allowPermanent = useStore((s) => s.prefs?.allowPermanentDelete ?? false);
  const caps = useStore((s) => s.caps);
  const width = useStore((s) => s.prefs?.monitorWidth ?? DEFAULT);
  const setPrefs = useStore((s) => s.setPrefs);
  const uiScale = useStore((s) => s.prefs?.uiScale ?? 1);
  const measuring = useStore((s) => s.measuring);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);

  const commitWidth = useCallback(
    (w: number) => {
      api.setMonitorWidth(w).then(setPrefs).catch(() => {});
    },
    [setPrefs],
  );

  const refresh = async () => {
    try {
      await api.monitorRefresh();
    } catch (err) {
      reportError('Could not re-measure', err);
    }
  };

  const deleteAll = (permanent: boolean) => {
    askConfirm({
      title: permanent
        ? `Delete ${monitor.count} watched items permanently?`
        : `${caps.trashVerb} for ${monitor.count} watched items?`,
      body: permanent
        ? `This frees ${formatBytes(monitor.total, base2)} and bypasses the ${caps.trashLabel}. There is no way to get these back.`
        : `This frees ${formatBytes(monitor.total, base2)}. You can put items back from the ${caps.trashLabel} until you empty it.`,
      confirmLabel: permanent ? 'Delete Permanently' : caps.trashVerb,
      destructive: permanent,
      onConfirm: async () => {
        try {
          const res = await api.monitorDeleteAll(permanent);
          if (res.succeeded?.length) {
            toast('success', `${res.succeeded.length} items removed`, `${formatBytes(res.bytes, base2)} reclaimed`);
          }
          const failed = Object.keys(res.failed ?? {});
          if (failed.length) toast('error', `${failed.length} could not be removed`, res.failed[failed[0]]);
        } catch (err) {
          reportError('Could not delete the watched items', err);
        }
      },
    });
  };

  const menuItems = (): MenuItem[] => [
    { label: 'Re-measure Now', onClick: refresh },
    { separator: true },
    { label: 'Stop Watching All', onClick: () => api.monitorClear().catch(() => {}) },
    { separator: true },
    { label: `${caps.trashVerb} — All`, destructive: true, onClick: () => deleteAll(false) },
    ...(allowPermanent
      ? [{ label: 'Delete All Permanently…', destructive: true, onClick: () => deleteAll(true) } as MenuItem]
      : []),
  ];

  const change = monitor.total - monitor.baseTotal;

  return (
    <ResizableColumn
      className="monitor-col"
      width={width}
      min={MIN}
      max={MAX}
      fallback={DEFAULT}
      uiScale={uiScale}
      onCommit={commitWidth}
      label="the monitor"
    >
      <aside className="monitor" aria-label="Monitor">
        <header className="panel-head">
          <h2>Monitor</h2>
          <div className="panel-head-actions">
            {monitor.count > 0 && (
              <>
                <button
                  className="icon-btn"
                  onClick={refresh}
                  disabled={!!measuring}
                  title="Measure every watched folder again"
                  aria-label="Re-measure the watched folders"
                >
                  ↻
                </button>
                <button
                  className="icon-btn"
                  onClick={(e) => setMenu({ x: e.clientX, y: e.clientY })}
                  title="More actions"
                  aria-label="More actions"
                >
                  ⋯
                </button>
              </>
            )}
            <button className="icon-btn" onClick={() => close(false)} aria-label="Close the monitor">
              ×
            </button>
          </div>
        </header>

        {monitor.count === 0 ? (
          <div className="panel-empty">
            <p>Nothing watched yet</p>
            <span>
              Add a folder from any scan with <kbd>{caps.modKey}D</kbd> or the right-click menu. It stays here across
              tabs, and re-measuring tells you what has changed since.
            </span>
          </div>
        ) : (
          <>
            <div className="panel-total">
              <strong>{formatBytes(monitor.total, base2)}</strong>
              <span>
                across {monitor.count} watched item{monitor.count === 1 ? '' : 's'}
              </span>
              {/* The reason the panel exists. Only once there is something to
                  say: before the first re-measure every item still weighs
                  what it did when it was added, and "no change" then means
                  "not looked at yet", which is not the same thing. */}
              {change !== 0 && (
                <span className={'monitor-change ' + (change > 0 ? 'monitor-up' : 'monitor-down')}>
                  {change > 0 ? '▲' : '▼'} {formatBytes(Math.abs(change), base2)} since watched
                </span>
              )}
            </div>

            {measuring && (
              <div className="monitor-measuring" title={measuring.path}>
                Measuring {measuring.index + 1} of {measuring.count} · {middleTruncate(measuring.path, 34)}
              </div>
            )}

            <div className="panel-list">
              {monitor.items.map((it) => {
                const d = it.size - it.base;
                return (
                  <div className={'panel-item' + (it.gone ? ' panel-item-gone' : '')} key={`${it.scanID}:${it.nodeID}`}>
                    <div className="panel-item-main">
                      <div className="panel-item-name">{it.name}</div>
                      <div className="panel-item-path" title={it.path}>
                        {middleTruncate(it.path, 52)}
                      </div>
                    </div>
                    <div className="panel-item-size">
                      {it.gone ? <span className="monitor-gone">gone</span> : formatBytes(it.size, base2)}
                      {!it.gone && d !== 0 && (
                        <span className={'monitor-delta ' + (d > 0 ? 'monitor-up' : 'monitor-down')}>
                          {d > 0 ? '+' : '−'}
                          {formatBytes(Math.abs(d), base2)}
                        </span>
                      )}
                    </div>
                    <button
                      className="icon-btn"
                      aria-label={`Stop watching ${it.name}`}
                      title="Stop watching"
                      onClick={() => api.monitorRemove(it.scanID, it.nodeID).catch(() => {})}
                    >
                      ×
                    </button>
                  </div>
                );
              })}
            </div>

            <footer className="panel-foot">
              <button className="btn btn-quiet" onClick={() => api.monitorClear().catch(() => {})}>
                Stop Watching All
              </button>
              <button className="btn" onClick={refresh} disabled={!!measuring}>
                {measuring ? 'Measuring…' : 'Re-measure'}
              </button>
            </footer>
          </>
        )}
      </aside>

      {menu && (
        <ContextMenu x={menu.x} y={menu.y} scale={uiScale} items={menuItems()} onClose={() => setMenu(null)} />
      )}
    </ResizableColumn>
  );
}
