import { useCallback, useEffect } from 'react';
import { api, on, ScanSummary } from './api';
import { paletteByName, resolveTheme } from './colors';
import { refreshCollector, reportError, useStore } from './store';
import { ConfirmSheet, TitleBar, Toasts, UpdateBanner } from './components/Chrome';
import { Collector } from './components/Collector';
import { HiddenSpace } from './components/HiddenSpace';
import { Preferences } from './components/Preferences';
import { ScanView } from './components/ScanView';
import { VolumePicker } from './components/VolumePicker';
import './styles.css';

export default function App() {
  const s = useStore();
  const prefs = s.prefs;
  const dark = s.theme === 'dark';
  const base2 = prefs?.base2 ?? false;
  const palette = paletteByName(prefs?.colorScheme ?? 'spectrum');

  // ---- initial state -----------------------------------------------------
  // Events emitted before React subscribes are dropped, so everything is
  // pulled once on mount and events are treated purely as deltas.
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const [volumes, p, scans, tray, helper, update, fda] = await Promise.all([
          api.listVolumes(),
          api.getPrefs(),
          api.listScans(),
          api.collectorList(),
          api.getHelperStatus(),
          api.getUpdateState(),
          api.checkFullDiskAccess(),
        ]);
        if (!alive) return;
        s.setVolumes(volumes);
        s.setPrefs(p);
        s.setScans(scans);
        s.setCollector(tray);
        s.setHelper(helper);
        s.setUpdate(update);
        s.setFDA(fda);
        s.setTheme(resolveTheme(p.theme));
        if (scans.length > 0) s.setActiveScan(scans[scans.length - 1].id);
        s.setReady(true);
      } catch (err) {
        reportError('DiskSpaceSaver could not start up cleanly', err);
        s.setReady(true);
      }
    })();
    return () => {
      alive = false;
    };
    // Intentionally once: this is startup, not a subscription.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- events ------------------------------------------------------------
  useEffect(() => {
    const unsubs = [
      on('volumes:changed', (v: any) => {
        if (Array.isArray(v)) s.setVolumes(v);
        else api.listVolumes().then(s.setVolumes).catch(() => {});
      }),
      on('scans:changed', (list: ScanSummary[]) => s.setScans(list ?? [])),
      on('scan:progress', (sum: ScanSummary) => sum && s.upsertScan(sum)),
      on('scan:done', (sum: ScanSummary) => {
        if (!sum) return;
        s.upsertScan(sum);
        if (sum.status === 'error' && sum.err) s.toast('error', `Scan of ${sum.label} failed`, sum.err);
      }),
      on('scan:opened', (id: string) => id && s.setActiveScan(id)),
      on('collector:changed', (c: any) => c && s.setCollector(c)),
      on('prefs:changed', (p: any) => {
        if (!p) return;
        s.setPrefs(p);
        s.setTheme(resolveTheme(p.theme));
      }),
      on('helper:changed', (h: any) => h && s.setHelper(h)),
      on('updater:changed', (u: any) => u && s.setUpdate(u)),
      on('app:error', (msg: string) => s.toast('error', 'Something went wrong', msg)),
    ];
    return () => unsubs.forEach((u) => u());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Follow the system appearance when the preference is "system".
  useEffect(() => {
    if (prefs?.theme !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const fn = () => s.setTheme(mq.matches ? 'dark' : 'light');
    mq.addEventListener('change', fn);
    return () => mq.removeEventListener('change', fn);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prefs?.theme]);

  useEffect(() => {
    document.documentElement.dataset.theme = s.theme;
  }, [s.theme]);

  // ---- actions -----------------------------------------------------------
  const startScan = useCallback(async (path: string, asAdmin: boolean) => {
    try {
      const id = await api.startScan(path, asAdmin);
      useStore.getState().setActiveScan(id);
      const list = await api.listScans();
      useStore.getState().setScans(list);
      useStore.getState().setActiveScan(id);
    } catch (err) {
      reportError('Could not start that scan', err);
    }
  }, []);

  const chooseFolder = useCallback(async () => {
    try {
      const dir = await api.chooseFolder();
      if (dir) startScan(dir, false);
    } catch (err) {
      reportError('Could not open the folder picker', err);
    }
  }, [startScan]);

  const closeScan = useCallback(async (id: string) => {
    try {
      await api.closeScan(id);
      useStore.getState().setScans(await api.listScans());
      await refreshCollector();
    } catch (err) {
      reportError('Could not close that scan', err);
    }
  }, []);

  // ---- keyboard ----------------------------------------------------------
  // The native menu carries the modifier shortcuts. Bare keys (Space, Return,
  // Backspace) are bound here instead, because as menu accelerators they would
  // swallow every keystroke in a text field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isTextInput(e.target)) return;
      const acts = (window as any).__dssActions ?? {};
      const meta = e.metaKey || e.ctrlKey;

      if (e.key === 'Escape') {
        const st = useStore.getState();
        if (st.confirm) st.askConfirm(null);
        else if (st.showPrefs) st.setShowPrefs(false);
        else if (st.hiddenSpaceMount) st.setHiddenSpaceMount(null);
        else if (st.showCollector) st.setShowCollector(false);
        else api.closeQuickLook();
        return;
      }
      if (e.key === ' ' && !meta) {
        e.preventDefault();
        acts.quickLook?.();
        return;
      }
      if (e.key === 'Enter' && !meta) {
        e.preventDefault();
        acts.zoomIn?.();
        return;
      }
      if (meta && e.key === 'ArrowUp') {
        e.preventDefault();
        acts.zoomOut?.();
        return;
      }
      if (meta && (e.key === 'Backspace' || e.key === 'Delete')) {
        e.preventDefault();
        if (e.altKey) acts.deleteForever?.();
        else acts.trash?.();
        return;
      }
      if (meta && e.key.toLowerCase() === 'd') {
        e.preventDefault();
        acts.collect?.();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Native menu commands route to the same action table the keyboard uses.
  useEffect(() => {
    return on('menu:command', (cmd: string) => {
      const st = useStore.getState();
      const acts = (window as any).__dssActions ?? {};
      switch (cmd) {
        case 'newScan':
        case 'home':
          st.setScreen('volumes');
          break;
        case 'scanFolder':
          chooseFolder();
          break;
        case 'scanAdmin':
          st.setScreen('volumes');
          st.toast('info', 'Pick a volume, then choose “As Admin”.');
          break;
        case 'closeScan':
          if (st.activeScan) closeScan(st.activeScan);
          break;
        case 'preferences':
        case 'showUpdates':
          st.setShowPrefs(true);
          break;
        case 'toggleCollector':
          st.setShowCollector(!st.showCollector);
          break;
        case 'hiddenSpace': {
          const scan = st.scans.find((x) => x.id === st.activeScan);
          const vol = st.volumes.find((v) => v.uuid && v.uuid === scan?.volumeUUID) ?? st.volumes[0];
          if (vol) st.setHiddenSpaceMount(vol.path);
          break;
        }
        default:
          acts[cmd]?.();
      }
    });
  }, [chooseFolder, closeScan]);

  const activeScan = s.scans.find((x) => x.id === s.activeScan) ?? null;

  if (!s.ready) {
    return (
      <div className="boot">
        <div className="boot-spin" />
      </div>
    );
  }

  return (
    <div className={'app' + (s.showCollector ? ' with-collector' : '')} style={{ '--wails-drop-target': 'drop' } as any}>
      <TitleBar
        scans={s.scans}
        activeID={s.activeScan}
        base2={base2}
        onSelect={(id) => s.setActiveScan(id)}
        onClose={closeScan}
        onNew={() => s.setScreen('volumes')}
      />
      <UpdateBanner />

      <main className="main">
        {s.screen === 'scan' && activeScan ? (
          <ScanView key={activeScan.id} scan={activeScan} palette={palette} dark={dark} />
        ) : (
          <VolumePicker
            volumes={s.volumes}
            base2={base2}
            helperAvailable={!!(s.helper?.running && s.helper.compatible)}
            onScan={startScan}
            onOpenCached={async (path) => {
              try {
                const id = await api.openCachedScan(path);
                s.setScans(await api.listScans());
                s.setActiveScan(id);
              } catch (err) {
                reportError('Could not open the saved scan', err);
              }
            }}
            onChooseFolder={chooseFolder}
            onHiddenSpace={(mount) => s.setHiddenSpaceMount(mount)}
          />
        )}

        {s.showCollector && <Collector base2={base2} />}
      </main>

      {s.showPrefs && <Preferences />}
      {s.hiddenSpaceMount && <HiddenSpace mount={s.hiddenSpaceMount} base2={base2} />}
      <ConfirmSheet />
      <Toasts />
    </div>
  );
}

function isTextInput(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  const tag = t.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable;
}
