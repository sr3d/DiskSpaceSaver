// The drop tray. Items gathered from any scan, with the total they would
// reclaim and one button to get rid of all of them.
import { api } from '../api';
import { formatBytes, middleTruncate } from '../format';
import { reportError, useStore } from '../store';

interface Props {
  base2: boolean;
}

export function Collector({ base2 }: Props) {
  const collector = useStore((s) => s.collector);
  const close = useStore((s) => s.setShowCollector);
  const askConfirm = useStore((s) => s.askConfirm);
  const toast = useStore((s) => s.toast);
  const allowPermanent = useStore((s) => s.prefs?.allowPermanentDelete ?? false);
  const caps = useStore((s) => s.caps);

  const deleteAll = (permanent: boolean) => {
    askConfirm({
      title: permanent
        ? `Delete ${collector.count} collected items permanently?`
        : `${caps.trashVerb} for ${collector.count} collected items?`,
      body: permanent
        ? `This frees ${formatBytes(collector.total, base2)} and bypasses the Trash. There is no way to get these back.`
        : `This frees ${formatBytes(collector.total, base2)}. You can put items back from the ${caps.trashLabel} until you empty it.`,
      confirmLabel: permanent ? 'Delete Permanently' : caps.trashVerb,
      destructive: permanent,
      onConfirm: async () => {
        try {
          const res = await api.collectorDeleteAll(permanent);
          if (res.succeeded?.length) {
            toast('success', `${res.succeeded.length} items removed`, `${formatBytes(res.bytes, base2)} reclaimed`);
          }
          const failed = Object.keys(res.failed ?? {});
          if (failed.length) toast('error', `${failed.length} could not be removed`, res.failed[failed[0]]);
        } catch (err) {
          reportError('Could not empty the collector', err);
        }
      },
    });
  };

  return (
    <aside className="collector" aria-label="Collector">
      <header className="collector-head">
        <h2>Collector</h2>
        <button className="icon-btn" onClick={() => close(false)} aria-label="Close collector">
          ×
        </button>
      </header>

      {collector.count === 0 ? (
        <div className="collector-empty">
          <p>Nothing collected yet</p>
          <span>
            Add items from any scan with <kbd>{caps.modKey}D</kbd> or the right-click menu. They stay here while you keep
            looking, across every tab.
          </span>
        </div>
      ) : (
        <>
          <div className="collector-total">
            <strong>{formatBytes(collector.total, base2)}</strong>
            <span>
              in {collector.count} item{collector.count === 1 ? '' : 's'}
            </span>
          </div>

          <div className="collector-list">
            {collector.items.map((it) => (
              <div className="collected" key={`${it.scanID}:${it.nodeID}`}>
                <div className="collected-main">
                  <div className="collected-name">{it.name}</div>
                  <div className="collected-path" title={it.path}>
                    {middleTruncate(it.path, 52)}
                  </div>
                </div>
                <div className="collected-size">{formatBytes(it.size, base2)}</div>
                <button
                  className="icon-btn"
                  aria-label={`Remove ${it.name} from the collector`}
                  onClick={() => api.collectorRemove(it.scanID, it.nodeID).catch(() => {})}
                >
                  ×
                </button>
              </div>
            ))}
          </div>

          <footer className="collector-foot">
            <button className="btn btn-quiet" onClick={() => api.collectorClear().catch(() => {})}>
              Clear
            </button>
            <button className="btn btn-danger" onClick={() => deleteAll(false)}>
              {`${caps.trashVerb} — All`}
            </button>
            {allowPermanent && (
              <button className="btn btn-danger-outline" onClick={() => deleteAll(true)}>
                Delete All…
              </button>
            )}
          </footer>
        </>
      )}
    </aside>
  );
}
