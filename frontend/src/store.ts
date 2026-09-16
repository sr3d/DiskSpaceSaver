// Application state.
//
// Deliberately excludes tree data: viewports are fetched on demand and held in
// per-tab state, the chart draws them imperatively, and the Go side remains
// the single owner of the tree.
import { create } from 'zustand';
import {
  api,
  CollectorState,
  HelperStatus,
  Prefs,
  ScanSummary,
  UpdateState,
  ViewportSlice,
  Volume,
  FDAStatus,
} from './api';

export type Screen = 'volumes' | 'scan';

export interface Toast {
  id: number;
  kind: 'info' | 'error' | 'success';
  text: string;
  detail?: string;
}

// TabState is the per-scan view state: where the user is looking, what is
// selected. Kept out of the tree so closing and reopening a tab is cheap.
export interface TabState {
  focus: number;
  selected: number;
  hovered: number;
  viewport?: ViewportSlice;
  loading: boolean;
}

export interface Confirm {
  title: string;
  body: string;
  confirmLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
}

interface State {
  ready: boolean;
  screen: Screen;
  volumes: Volume[];
  scans: ScanSummary[];
  activeScan: string | null;
  tabs: Record<string, TabState>;
  collector: CollectorState;
  prefs: Prefs | null;
  helper: HelperStatus | null;
  update: UpdateState | null;
  fda: FDAStatus | null;
  toasts: Toast[];
  showPrefs: boolean;
  showCollector: boolean;
  hiddenSpaceMount: string | null;
  confirm: Confirm | null;
  theme: 'light' | 'dark';

  setReady(v: boolean): void;
  setScreen(s: Screen): void;
  setVolumes(v: Volume[]): void;
  setScans(s: ScanSummary[]): void;
  upsertScan(s: ScanSummary): void;
  setActiveScan(id: string | null): void;
  tab(id: string): TabState;
  patchTab(id: string, patch: Partial<TabState>): void;
  dropTab(id: string): void;
  setCollector(c: CollectorState): void;
  setPrefs(p: Prefs): void;
  setHelper(h: HelperStatus): void;
  setUpdate(u: UpdateState): void;
  setFDA(f: FDAStatus): void;
  setTheme(t: 'light' | 'dark'): void;
  toast(kind: Toast['kind'], text: string, detail?: string): void;
  dismissToast(id: number): void;
  setShowPrefs(v: boolean): void;
  setShowCollector(v: boolean): void;
  setHiddenSpaceMount(m: string | null): void;
  askConfirm(c: Confirm | null): void;
}

export const emptyTab = (): TabState => ({
  focus: 1,
  selected: 0,
  hovered: 0,
  loading: true,
});

let toastSeq = 1;

export const useStore = create<State>((set, get) => ({
  ready: false,
  screen: 'volumes',
  volumes: [],
  scans: [],
  activeScan: null,
  tabs: {},
  collector: { items: [], total: 0, count: 0 },
  prefs: null,
  helper: null,
  update: null,
  fda: null,
  toasts: [],
  showPrefs: false,
  showCollector: false,
  hiddenSpaceMount: null,
  confirm: null,
  theme: 'dark',

  setReady: (v) => set({ ready: v }),
  setScreen: (screen) => set({ screen }),
  setVolumes: (volumes) => set({ volumes }),

  setScans: (scans) =>
    set((st) => {
      // Drop per-tab view state for scans the backend no longer has, so a
      // closed tab cannot leak its focus into a later scan with the same id.
      const live = new Set(scans.map((s) => s.id));
      const tabs: Record<string, TabState> = {};
      for (const [id, t] of Object.entries(st.tabs)) if (live.has(id)) tabs[id] = t;
      let active = st.activeScan;
      if (active && !live.has(active)) active = scans.length ? scans[scans.length - 1].id : null;
      return { scans, tabs, activeScan: active, screen: active ? st.screen : 'volumes' };
    }),

  upsertScan: (s) =>
    set((st) => {
      const i = st.scans.findIndex((x) => x.id === s.id);
      if (i < 0) return { scans: [...st.scans, s] };
      const scans = st.scans.slice();
      scans[i] = s;
      return { scans };
    }),

  setActiveScan: (id) =>
    set((st) => ({
      activeScan: id,
      screen: id ? 'scan' : 'volumes',
      tabs: id && !st.tabs[id] ? { ...st.tabs, [id]: emptyTab() } : st.tabs,
    })),

  tab: (id) => get().tabs[id] ?? emptyTab(),

  patchTab: (id, patch) =>
    set((st) => ({
      tabs: { ...st.tabs, [id]: { ...(st.tabs[id] ?? emptyTab()), ...patch } },
    })),

  dropTab: (id) =>
    set((st) => {
      const tabs = { ...st.tabs };
      delete tabs[id];
      return { tabs };
    }),

  setCollector: (collector) => set({ collector }),
  setPrefs: (prefs) => set({ prefs }),
  setHelper: (helper) => set({ helper }),
  setUpdate: (update) => set({ update }),
  setFDA: (fda) => set({ fda }),
  setTheme: (theme) => set({ theme }),

  toast: (kind, text, detail) =>
    set((st) => {
      const id = toastSeq++;
      // Errors stay until dismissed; everything else clears itself.
      if (kind !== 'error') {
        setTimeout(() => get().dismissToast(id), 4200);
      }
      return { toasts: [...st.toasts, { id, kind, text, detail }].slice(-4) };
    }),

  dismissToast: (id) => set((st) => ({ toasts: st.toasts.filter((t) => t.id !== id) })),
  setShowPrefs: (showPrefs) => set({ showPrefs }),
  setShowCollector: (showCollector) => set({ showCollector }),
  setHiddenSpaceMount: (hiddenSpaceMount) => set({ hiddenSpaceMount }),
  askConfirm: (confirm) => set({ confirm }),
}));

// refreshCollector pulls the tray from Go.
export async function refreshCollector() {
  try {
    useStore.getState().setCollector(await api.collectorList());
  } catch {
    /* the tray is not worth an error toast */
  }
}

// reportError turns a rejected binding into a toast the user can read.
export function reportError(prefix: string, err: unknown) {
  const msg = err instanceof Error ? err.message : String(err);
  useStore.getState().toast('error', prefix, msg);
}
