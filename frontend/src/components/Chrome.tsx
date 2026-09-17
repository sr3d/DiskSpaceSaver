// Window chrome: the draggable title bar, scan tabs, toasts and the confirm
// sheet.
import { ScanSummary } from '../api';
import { formatBytes } from '../format';
import { useStore } from '../store';
import { api } from '../api';
import appIcon from '../assets/images/appicon.svg';

interface TabsProps {
  scans: ScanSummary[];
  activeID: string | null;
  base2: boolean;
  onSelect(id: string): void;
  onClose(id: string): void;
  onNew(): void;
}

export function TitleBar({ scans, activeID, base2, onSelect, onClose, onNew }: TabsProps) {
  const monitor = useStore((s) => s.monitor);
  const setShowMonitor = useStore((s) => s.setShowMonitor);
  const showMonitor = useStore((s) => s.showMonitor);
  const setShowPrefs = useStore((s) => s.setShowPrefs);
  const setScreen = useStore((s) => s.setScreen);
  const mod = useStore((s) => s.caps.modKey);

  return (
    // --wails-draggable makes the strip behave as the window's title bar; the
    // left inset keeps clear of the traffic lights.
    <div className="titlebar" style={{ '--wails-draggable': 'drag' } as any}>
      <div className="titlebar-inset" />
      <button
        className="tab tab-home"
        onClick={() => setScreen('volumes')}
        title="All volumes"
        aria-label="All volumes"
      >
        <img src={appIcon} alt="" width={20} height={20} />
      </button>

      <div className="tabs">
        {scans.map((s) => (
          <div
            key={s.id}
            className={'tab' + (s.id === activeID ? ' tab-active' : '')}
            onClick={() => onSelect(s.id)}
            title={s.root}
          >
            {s.status === 'scanning' && <span className="tab-spin" aria-hidden />}
            {s.asAdmin && <span className="tab-admin" title="Scanned as administrator">⚿</span>}
            <span className="tab-label">{s.label}</span>
            <span className="tab-size">{formatBytes(s.bytes, base2)}</span>
            <button
              className="tab-close"
              aria-label={`Close ${s.label}`}
              onClick={(e) => {
                e.stopPropagation();
                onClose(s.id);
              }}
            >
              ×
            </button>
            {/* A determinate line only. When the fraction is unknown the
                spinner above already says "running", and a second animation
                sweeping under every tab would be noise. Purely decorative:
                the scan's own bar carries the accessible reading. */}
            {s.status === 'scanning' && s.fraction >= 0 && (
              <div className="pbar tab-bar" aria-hidden>
                <div className="pbar-fill" style={{ width: `${Math.min(1, s.fraction) * 100}%` }} />
              </div>
            )}
          </div>
        ))}
        <button className="tab tab-new" onClick={onNew} title={`New scan (${mod}T)`}>
          +
        </button>
      </div>

      <div className="titlebar-right">
        <button
          className={'chip' + (monitor.count ? ' chip-active' : '') + (showMonitor ? ' chip-open' : '')}
          onClick={() => setShowMonitor(!showMonitor)}
          title={`Monitor — watch folders and see how they change (${mod}L)`}
        >
          Monitor{monitor.count > 0 && <em>{formatBytes(monitor.total, base2)}</em>}
        </button>
        {/* The gear alone was a guess: nothing in the bar said what it opened.
            Labelled like the Monitor beside it, so both read as the same
            kind of control. */}
        <button className="chip" onClick={() => setShowPrefs(true)} title={`Settings (${mod},)`}>
          <span aria-hidden>⚙</span>
          Settings
        </button>
      </div>
    </div>
  );
}

export function Toasts() {
  const toasts = useStore((s) => s.toasts);
  const dismiss = useStore((s) => s.dismissToast);
  if (toasts.length === 0) return null;
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast-${t.kind}`}>
          <div className="toast-body">
            <div className="toast-text">{t.text}</div>
            {t.detail && <div className="toast-detail">{t.detail}</div>}
          </div>
          <button className="icon-btn" onClick={() => dismiss(t.id)} aria-label="Dismiss">
            ×
          </button>
        </div>
      ))}
    </div>
  );
}

export function ConfirmSheet() {
  const confirm = useStore((s) => s.confirm);
  const ask = useStore((s) => s.askConfirm);
  if (!confirm) return null;
  return (
    <div className="sheet-backdrop" onMouseDown={() => ask(null)}>
      <div className="sheet sheet-narrow" onMouseDown={(e) => e.stopPropagation()} role="alertdialog">
        <div className="confirm-body">
          <h2>{confirm.title}</h2>
          <p>{confirm.body}</p>
        </div>
        <div className="confirm-actions">
          <button className="btn" onClick={() => ask(null)} autoFocus>
            Cancel
          </button>
          <button
            className={confirm.destructive ? 'btn btn-danger' : 'btn btn-primary'}
            onClick={() => {
              const fn = confirm.onConfirm;
              ask(null);
              fn();
            }}
          >
            {confirm.confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

export function UpdateBanner() {
  const update = useStore((s) => s.update);
  const toast = useStore((s) => s.toast);
  if (!update?.updateAvailable) return null;
  return (
    <div className="update-banner">
      <div>
        <strong>Version {update.latestVersion} is available</strong>
        {update.releaseNotes && <span className="update-notes">{update.releaseNotes}</span>}
      </div>
      <button
        className="btn btn-primary"
        onClick={() =>
          api.installUpdate().catch((e) => toast('error', 'Could not install the update', String(e)))
        }
      >
        Install and Relaunch
      </button>
    </div>
  );
}
