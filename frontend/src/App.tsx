import { useCallback, useEffect } from 'react';
import { api, on, onFileDrop, ScanSummary } from './api';
import { paletteByName, resolveTheme } from './colors';
import { refreshMonitor, reportError, useStore } from './store';
import { ConfirmSheet, TitleBar, Toasts, UpdateBanner } from './components/Chrome';
import { Monitor } from './components/Monitor';
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
      // allSettled, not all: getHelperStatus and checkFullAccess are the two
      // platform-specific calls here, and a rejection from either used to take
      // volumes, preferences, scans, the monitor and the theme down with it,
      // leaving an app that looked broken for a reason that did not matter.
      const [volumes, p, scans, tray, helper, update, fda, caps] = await Promise.allSettled([
        api.listVolumes(),
        api.getPrefs(),
        api.listScans(),
        api.monitorList(),
        api.getHelperStatus(),
        api.getUpdateState(),
        api.checkFullDiskAccess(),
        api.getCapabilities(),
      ]);
      if (!alive) return;

      const take = <T,>(r: PromiseSettledResult<T>, fallback: T): T =>
        r.status === 'fulfilled' ? r.value : fallback;

      if (caps.status === 'fulfilled') s.setCaps(caps.value);
      if (volumes.status === 'fulfilled') s.setVolumes(volumes.value);
      if (tray.status === 'fulfilled') s.setMonitor(tray.value);
      if (helper.status === 'fulfilled') s.setHelper(helper.value);
      if (update.status === 'fulfilled') s.setUpdate(update.value);
      if (fda.status === 'fulfilled') s.setFDA(fda.value);

      if (p.status === 'fulfilled') {
        s.setPrefs(p.value);
        s.setTheme(resolveTheme(p.value.theme));
      } else {
        reportError('Could not read your preferences', p.reason);
      }

      const list = take(scans, []);
      s.setScans(list);
      if (list.length > 0) s.setActiveScan(list[list.length - 1].id);

      // Only the things the app cannot work without are worth a message.
      if (volumes.status === 'rejected') {
        reportError('Could not list your drives', volumes.reason);
      }
      s.setReady(true);
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
      on('monitor:changed', (c: any) => c && s.setMonitor(c)),
      // A re-measure is several full walks in a row, so it reports which
      // one it is on; `done` is the only way the panel learns it is over,
      // since the last walk sends no further progress.
      on('monitor:measuring', (m: any) =>
        s.setMeasuring(m && !m.done ? { path: m.path ?? '', index: m.index ?? 0, count: m.count ?? 0 } : null),
      ),
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

  useEffect(() => {
    document.documentElement.dataset.os = s.caps.os;
  }, [s.caps.os]);

  // Interface zoom. CSS `zoom` scales layout rather than painting over it, so
  // text stays sharp and everything reflows at the new size — unlike a
  // transform, which would leave the element's box at its old dimensions.
  // --ui-scale is published alongside so the few pieces that must keep their
  // physical size, notably the gap left for the window's traffic lights, can
  // divide it back out.
  const uiScale = prefs?.uiScale ?? 1;
  useEffect(() => {
    const root = document.documentElement;
    root.style.zoom = String(uiScale);
    root.style.setProperty('--ui-scale', String(uiScale));

    // Breakpoints have to be measured in the space the layout actually gets,
    // which is the window divided by the scale. A media query would only see
    // the window, so at 200% on a large display it would report "plenty of
    // room" while the panels had none.
    const apply = () => {
      const effective = window.innerWidth / (uiScale || 1);
      const tier = effective < 760 ? 'narrow' : effective < 1180 ? 'medium' : 'wide';
      root.dataset.layout = tier;
      useStore.getState().setLayout(tier);
    };
    apply();
    window.addEventListener('resize', apply);
    return () => window.removeEventListener('resize', apply);
  }, [uiScale]);

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

  // Dropping a folder on the window scans it. The window has advertised
  // itself as a drop target since the first build and Go enables file drop,
  // but nothing was listening, so a drop did nothing — "Add a Folder" was the
  // only way in.
  useEffect(() => {
    return onFileDrop(async (paths) => {
      if (!paths.length) return;
      try {
        const dirs = await api.dropTargets(paths);
        // One tab per dropped folder, but not a dozen from a careless drop.
        for (const dir of dirs.slice(0, 4)) await startScan(dir, false);
      } catch (err) {
        reportError('Could not scan what was dropped', err);
      }
    });
  }, [startScan]);

  const chooseFolder = useCallback(async () => {
    try {
      const dir = await api.chooseFolder();
      if (dir) startScan(dir, false);
    } catch (err) {
      reportError('Could not open the folder picker', err);
    }
  }, [startScan]);

  // The menu, the keyboard and the Preferences control all step the same
  // ladder, which lives in Go, so they cannot disagree about what comes next.
  const zoomUI = useCallback(async (direction: number, reset = false) => {
    try {
      useStore.getState().setPrefs(await api.zoomUI(direction, reset));
    } catch (err) {
      reportError('Could not change the interface size', err);
    }
  }, []);

  const closeScan = useCallback(async (id: string) => {
    try {
      await api.closeScan(id);
      useStore.getState().setScans(await api.listScans());
      await refreshMonitor();
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
        else if (st.screen === 'scan' && st.activeScan && st.tabs[st.activeScan]?.selectionOpen)
          st.patchTab(st.activeScan, { selectionOpen: false });
        else if (st.showMonitor) st.setShowMonitor(false);
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
      // Backspace goes back, as it does in Explorer, and the forward-delete
      // key does the same — on a Mac keyboard the one key sitting there is
      // labelled "delete", and either name should reach the same place.
      //
      // Bare presses only: ⌘⌫ above is Move to Trash, and that has to stay the
      // unambiguous one. Held back while a sheet is up, because navigating the
      // view behind a question the user has not answered yet is never what the
      // key meant.
      if ((e.key === 'Backspace' || e.key === 'Delete') && !meta && !e.altKey && !e.shiftKey) {
        e.preventDefault();
        const st = useStore.getState();
        if (st.confirm || st.showPrefs || st.hiddenSpaceMount) return;
        acts.back?.();
        return;
      }
      if (meta && e.key.toLowerCase() === 'd') {
        e.preventDefault();
        acts.watch?.();
        return;
      }
      // Interface size. In the packaged app the menu takes these first and
      // they never reach here; this covers the shifted "+" that the menu does
      // not claim, and the browser during development.
      if (meta && (e.key === '=' || e.key === '+')) {
        e.preventDefault();
        (window as any).__dssZoomUI?.(+1);
        return;
      }
      if (meta && (e.key === '-' || e.key === '_')) {
        e.preventDefault();
        (window as any).__dssZoomUI?.(-1);
        return;
      }
      if (meta && e.key === '0') {
        e.preventDefault();
        (window as any).__dssZoomUI?.(0, true);
        return;
      }
      // The macOS convention for back and forward in a browsing view.
      if (meta && e.key === '[') {
        e.preventDefault();
        acts.back?.();
        return;
      }
      if (meta && e.key === ']') {
        e.preventDefault();
        acts.forward?.();
        return;
      }
      // Windows spells back and forward as Alt with the arrow keys, and delete
      // without a modifier. Both sets are bound everywhere rather than being
      // switched on the platform: an extra binding costs nothing, and people
      // who move between the two do not have to relearn.
      if (e.altKey && !meta && e.key === 'ArrowLeft') {
        e.preventDefault();
        acts.back?.();
        return;
      }
      if (e.altKey && !meta && e.key === 'ArrowRight') {
        e.preventDefault();
        acts.forward?.();
        return;
      }
      if (!meta && !e.altKey && e.key === 'Delete') {
        e.preventDefault();
        if (e.shiftKey) acts.deleteForever?.();
        else acts.trash?.();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // The side buttons on a mouse, as buttons 3 and 4. This is the Windows
  // path: WebView2 delivers them to the DOM, and the default action there is
  // to navigate the view's own history — which here would leave the app
  // entirely — so the press is swallowed as well as acted on. `auxclick` alone
  // is not enough: the navigation is triggered by mousedown, before any click
  // event exists.
  //
  // On macOS these never arrive. A native event monitor takes them off the
  // queue first, because what WebKit does with them is not ours to rely on,
  // and sends back and forward through `menu:command` instead.
  useEffect(() => {
    const swallow = (e: MouseEvent) => {
      if (e.button === 3 || e.button === 4) e.preventDefault();
    };
    const onAux = (e: MouseEvent) => {
      if (e.button !== 3 && e.button !== 4) return;
      e.preventDefault();
      const acts = (window as any).__dssActions ?? {};
      if (e.button === 3) acts.back?.();
      else acts.forward?.();
    };
    window.addEventListener('mousedown', swallow);
    window.addEventListener('mouseup', onAux);
    window.addEventListener('auxclick', swallow);
    return () => {
      window.removeEventListener('mousedown', swallow);
      window.removeEventListener('mouseup', onAux);
      window.removeEventListener('auxclick', swallow);
    };
  }, []);

  useEffect(() => {
    (window as any).__dssZoomUI = zoomUI;
    return () => {
      delete (window as any).__dssZoomUI;
    };
  }, [zoomUI]);

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
        case 'uiBigger':
          zoomUI(+1);
          break;
        case 'uiSmaller':
          zoomUI(-1);
          break;
        case 'uiReset':
          zoomUI(0, true);
          break;
        case 'toggleMonitor':
          st.setShowMonitor(!st.showMonitor);
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
  }, [chooseFolder, closeScan, zoomUI]);

  const activeScan = s.scans.find((x) => x.id === s.activeScan) ?? null;

  if (!s.ready) {
    return (
      <div className="boot">
        <div className="boot-spin" />
      </div>
    );
  }

  return (
    <div className={'app' + (s.showMonitor ? ' with-monitor' : '')} style={{ '--wails-drop-target': 'drop' } as any}>
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

        {s.showMonitor && <Monitor base2={base2} />}
      </main>

      {/* Drop feedback. The Wails runtime already marks every element whose
          computed --wails-drop-target is "drop" with .wails-drop-target-active
          while a file drag is over the window, and clears it on leave or drop,
          so this needs no listener of its own — only CSS keyed off the class
          the runtime sets on .app.

          aria-hidden because it is transient feedback during a pointer drag,
          with no keyboard equivalent to announce; "Add a Folder" is the path
          that does not need a mouse. */}
      <div className="dropzone" aria-hidden>
        <div className="dropzone-card">
          <strong>Drop to scan</strong>
          <span>A folder opens as a new scan · a file opens the folder it is in</span>
        </div>
      </div>

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
