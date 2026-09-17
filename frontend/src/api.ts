// Thin, typed surface over the Wails bindings.
//
// Nothing here holds tree data: the Go side owns the tree and hands back
// viewport slices keyed by opaque uint32 handles. A handle of 0 is the
// synthetic "other" wedge and must never be passed to an action.
import * as App from '../wailsjs/go/main/App';
import { EventsOn, EventsEmit, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';

export type Kind = 0 | 1 | 2 | 3 | 4; // file, dir, symlink, package, other

export const KIND_FILE = 0;
export const KIND_DIR = 1;
export const KIND_SYMLINK = 2;
export const KIND_PACKAGE = 3;
export const KIND_OTHER = 4;

// Entry flags, mirroring internal/scanner and internal/tree.
export const FLAG_DUP = 1 << 0;
export const FLAG_HIDDEN = 1 << 1;
export const FLAG_MOUNTPT = 1 << 2;
export const FLAG_EXCLUDED = 1 << 3;
export const FLAG_NOPERM = 1 << 4;
export const FLAG_CLOUD = 1 << 5;
export const FLAG_DELETED = 1 << 6;

export interface Volume {
  name: string;
  path: string;
  uuid: string;
  total: number;
  free: number;
  availRaw: number;
  purgeable: number;
  used: number;
  removable: boolean;
  internal: boolean;
  network: boolean;
  readOnly: boolean;
  isRoot: boolean;
  fs: string;
  icon?: string;
}

export interface VNode {
  id: number;
  name: string;
  size: number;
  logical: number;
  kind: number;
  flags: number;
  files: number;
  dirs: number;
  mtime: number;
  hasKids: boolean;
  other?: number;
  err?: string;
  children?: VNode[];
}

export interface ViewportSlice {
  focus: VNode;
  ancestors: VNode[];
  rootTotal: number;
  scanning: boolean;
  sorted: boolean;
}

export interface NodeSummary {
  id: number;
  name: string;
  size: number;
  logical: number;
  kind: number;
  flags: number;
  files: number;
  dirs: number;
  mtime: number;
  hasKids: boolean;
  fraction: number;
  err?: string;
}

export type ScanStatus = 'scanning' | 'done' | 'cancelled' | 'error';

export interface ScanSummary {
  id: string;
  root: string;
  label: string;
  volumeUUID?: string;
  asAdmin: boolean;
  status: ScanStatus;
  files: number;
  dirs: number;
  bytes: number;
  inaccessible: number;
  currentPath?: string;
  stalled: boolean;
  stalledMs?: number;
  startedAt: number;
  finishedAt?: number;
  fromCache: boolean;
  cachedAt?: number;
  err?: string;
  /** What this scan is expected to account for, in bytes; 0 when unknown. */
  expectedBytes: number;
  /**
   * Completion in [0,1], or -1 when it cannot be honestly estimated. Resolved
   * in Go, because the taskbar indicator needs the same number and both of the
   * denominators it can draw on already live there.
   */
  fraction: number;
}

/** A subtree rescan, which has no scan of its own to report progress against. */
export interface SubProgress {
  scanID: string;
  nodeID: number;
  path?: string;
  files?: number;
  dirs?: number;
  bytes?: number;
  fraction: number;
  done: boolean;
}

export interface ItemRef {
  scanID: string;
  nodeID: number;
}

/**
 * One ticked row, described by Go. The pane used to be handed rows filtered
 * out of the visible list, which is why a tick could not outlive a change of
 * folder; these carry everything needed to show an item from anywhere.
 */
export interface SelectedItem {
  scanID: string;
  nodeID: number;
  name: string;
  path: string;
  size: number;
  kind: number;
  flags: number;
  files: number;
  /** The handle is no longer in the tree — deleted, or pruned by a rescan. */
  gone: boolean;
  /** Inside another selected item, so its bytes are already counted there. */
  covered: boolean;
  err?: string;
}

export interface MonitorItem {
  scanID: string;
  nodeID: number;
  path: string;
  name: string;
  size: number;
  kind: number;
  scanRoot: string;
  asAdmin: boolean;
  /** What it weighed when first watched, and when that was (unix seconds). */
  base: number;
  addedAt: number;
  /** Not found at the last re-measure. Kept listed: disappearing is a result. */
  gone: boolean;
  /** When size was last confirmed against the disk; 0 if never. */
  measuredAt: number;
}

export interface MonitorState {
  items: MonitorItem[];
  total: number;
  count: number;
  /** What these weighed between them when each was first watched. */
  baseTotal: number;
}

export interface OpResult {
  succeeded: string[];
  failed: Record<string, string>;
  bytes: number;
}

export interface Prefs {
  base2: boolean;
  theme: 'system' | 'light' | 'dark';
  colorScheme: string;
  showHidden: boolean;
  exclusions: string[];
  updateChannel: 'stable' | 'beta';
  autoCheckUpdates: boolean;
  sendStats: boolean;
  installID: string;
  allowPermanentDelete: boolean;
  confirmTrash: boolean;
  useCache: boolean;
  useBulkScan: boolean;
  minArcFraction: number;
  rings: number;
  uiScale: number;
  minItemSize: number;
  sidebarWidth: number;
  selectionWidth: number;
  monitorWidth: number;
}

/** A page of children plus what the size filter withheld. */
export interface ChildPage {
  rows: NodeSummary[];
  total: number;
  hidden: number;
  hiddenBytes: number;
}

export interface Preview {
  nodeID: number;
  name: string;
  path: string;
  kind: number;
  size: number;
  mtime: number;
  /** PNG data URI, empty when the system cannot render this file. */
  image: string;
  utType: string;
  ext: string;
}

export interface SnapshotInfo {
  name: string;
  date: string;
}

export interface HiddenSpaceReport {
  volume: string;
  mount: string;
  total: number;
  used: number;
  free: number;
  visible: number;
  hasScan: boolean;
  scanning: boolean;
  purgeable: number;
  snapshots: SnapshotInfo[];
  snapshotBytes: number;
  protected: number;
  inaccessible: number;
  unaccounted: number;
  notes: string[];
}

export interface HelperStatus {
  installed: boolean;
  running: boolean;
  version: number;
  build: string;
  compatible: boolean;
  registered: string;
  err?: string;
  registration: string;
  needsApproval: boolean;
  bundled: boolean;
  advice?: string;
}

export interface UpdateState {
  supported: boolean;
  checking: boolean;
  updateAvailable: boolean;
  currentVersion: string;
  latestVersion?: string;
  releaseNotes?: string;
  downloadURL?: string;
  lastChecked?: number;
  err?: string;
  reason?: string;
}

export interface FDAStatus {
  granted: boolean;
  probe: string;
  detail?: string;
}

export interface CachedScan {
  found: boolean;
  key: string;
  root: string;
  savedAt: number;
  nodes: number;
  bytes: number;
  ageMillis: number;
}

/**
 * What this platform can actually do. Several macOS features have no Windows
 * counterpart, so rather than showing controls that quietly do nothing, the
 * interface reads this once and leaves them out.
 */
export interface Caps {
  os: 'darwin' | 'windows';
  preview: boolean;
  properties: boolean;
  snapshots: boolean;
  purgeable: boolean;
  helper: boolean;
  helperInstall: boolean;
  fullDiskAccess: boolean;
  revealLabel: string;
  trashLabel: string;
  trashVerb: string;
  propertiesLabel: string;
  modKey: string;
}

/** Sensible answers before the real ones arrive, and if the call ever fails. */
export const DEFAULT_CAPS: Caps = {
  os: 'darwin',
  preview: true,
  properties: true,
  snapshots: true,
  purgeable: true,
  helper: true,
  helperInstall: true,
  fullDiskAccess: true,
  revealLabel: 'Show in Finder',
  trashLabel: 'Trash',
  trashVerb: 'Move to Trash',
  propertiesLabel: 'Get Info',
  modKey: '\u2318',
};

export interface AppInfo {
  version: string;
  build: string;
  bundled: boolean;
  bundleID: string;
  teamID: string;
  caps: Caps;
}

// -1 for depth and minFraction tells Go to use the user's preferences.
export const PREF_DEFAULT = -1;

export const api = {
  listVolumes: (): Promise<Volume[]> => App.ListVolumes() as any,
  startScan: (path: string, asAdmin: boolean): Promise<string> => App.StartScan(path, asAdmin),
  cancelScan: (id: string) => App.CancelScan(id),
  listScans: (): Promise<ScanSummary[]> => App.ListScans() as any,
  closeScan: (id: string) => App.CloseScan(id),
  getScan: (id: string): Promise<ScanSummary> => App.GetScan(id) as any,

  lookupCachedScan: (path: string): Promise<CachedScan> => App.LookupCachedScan(path) as any,
  openCachedScan: (path: string): Promise<string> => App.OpenCachedScan(path),

  getViewport: (
    scanID: string,
    nodeID: number,
    depth = PREF_DEFAULT,
    minFraction = PREF_DEFAULT,
  ): Promise<ViewportSlice> => App.GetViewport(scanID, nodeID, depth, minFraction) as any,
  getChildren: (scanID: string, nodeID: number, offset: number, limit: number): Promise<ChildPage> =>
    App.GetChildren(scanID, nodeID, offset, limit) as any,
  previewItem: (ref: ItemRef, maxPoints = 320): Promise<Preview> =>
    App.PreviewItem(ref as any, maxPoints) as any,
  getChildCount: (scanID: string, nodeID: number): Promise<number> => App.GetChildCount(scanID, nodeID),
  getAncestors: (scanID: string, nodeID: number): Promise<NodeSummary[]> =>
    App.GetAncestors(scanID, nodeID) as any,
  getPath: (scanID: string, nodeID: number): Promise<string> => App.GetPath(scanID, nodeID),
  getLargest: (scanID: string, n: number): Promise<NodeSummary[]> => App.GetLargest(scanID, n) as any,
  findNode: (scanID: string, path: string): Promise<number> => App.FindNode(scanID, path),

  reveal: (refs: ItemRef[]) => App.RevealInFinder(refs as any),
  quickLook: (refs: ItemRef[], index: number) => App.QuickLook(refs as any, index),
  closeQuickLook: () => App.CloseQuickLook(),
  getInfo: (ref: ItemRef) => App.GetInfo(ref as any),
  openItem: (ref: ItemRef) => App.OpenItem(ref as any),
  copyPath: (ref: ItemRef): Promise<string> => App.CopyPath(ref as any),

  moveToTrash: (refs: ItemRef[]): Promise<OpResult> => App.MoveToTrash(refs as any) as any,
  deletePermanently: (refs: ItemRef[]): Promise<OpResult> => App.DeletePermanently(refs as any) as any,
  rescan: (scanID: string, nodeID: number): Promise<string> => App.Rescan(scanID, nodeID),

  monitorAdd: (refs: ItemRef[]): Promise<MonitorState> => App.MonitorAdd(refs as any) as any,
  monitorRemove: (scanID: string, nodeID: number): Promise<MonitorState> =>
    App.MonitorRemove(scanID, nodeID) as any,
  monitorList: (): Promise<MonitorState> => App.MonitorList() as any,
  monitorClear: (): Promise<MonitorState> => App.MonitorClear() as any,
  monitorRefresh: (): Promise<MonitorState> => App.MonitorRefresh() as any,
  monitorDeleteAll: (permanent: boolean): Promise<OpResult> => App.MonitorDeleteAll(permanent) as any,

  hiddenSpace: (mount: string): Promise<HiddenSpaceReport> => App.HiddenSpace(mount) as any,
  listLocalSnapshots: (mount: string): Promise<SnapshotInfo[]> => App.ListLocalSnapshots(mount) as any,
  deleteLocalSnapshots: (mount: string, names: string[]): Promise<OpResult> =>
    App.DeleteLocalSnapshots(mount, names) as any,
  openStorageSettings: () => App.OpenStorageSettings(),

  checkFullDiskAccess: (): Promise<FDAStatus> => App.CheckFullDiskAccess() as any,
  openFDASettings: () => App.OpenFDASettings(),
  chooseFolder: (): Promise<string> => App.ChooseFolder(),

  getPrefs: (): Promise<Prefs> => App.GetPrefs() as any,
  setPrefs: (p: Prefs): Promise<Prefs> => App.SetPrefs(p as any) as any,
  zoomUI: (direction: number, reset = false): Promise<Prefs> => App.ZoomUI(direction, reset) as any,
  setUIScale: (scale: number): Promise<Prefs> => App.SetUIScale(scale) as any,
  setMinItemSize: (bytes: number): Promise<Prefs> => App.SetMinItemSize(bytes) as any,
  setSidebarWidth: (width: number): Promise<Prefs> => App.SetSidebarWidth(width) as any,
  setSelectionWidth: (width: number): Promise<Prefs> => App.SetSelectionWidth(width) as any,
  setMonitorWidth: (width: number): Promise<Prefs> => App.SetMonitorWidth(width) as any,
  describeItems: (refs: ItemRef[]): Promise<SelectedItem[]> => App.DescribeItems(refs) as any,
  dropTargets: (paths: string[]): Promise<string[]> => App.DropTargets(paths) as any,
  addExclusion: (path: string): Promise<Prefs> => App.AddExclusion(path) as any,
  removeExclusion: (path: string): Promise<Prefs> => App.RemoveExclusion(path) as any,

  getHelperStatus: (): Promise<HelperStatus> => App.GetHelperStatus() as any,
  installHelper: (): Promise<HelperStatus> => App.InstallHelper() as any,
  uninstallHelper: (): Promise<HelperStatus> => App.UninstallHelper() as any,
  openLoginItemsSettings: () => App.OpenLoginItemsSettings(),

  getUpdateState: (): Promise<UpdateState> => App.GetUpdateState() as any,
  checkForUpdates: (): Promise<UpdateState> => App.CheckForUpdates() as any,
  installUpdate: () => App.InstallUpdate(),

  listCachedScans: () => App.ListCachedScans() as any,
  clearScanCache: () => App.ClearScanCache(),
  scanCacheBytes: (): Promise<number> => App.ScanCacheBytes(),

  getAppInfo: (): Promise<AppInfo> => App.GetAppInfo() as any,
  getCapabilities: (): Promise<Caps> => App.GetCapabilities() as any,
};

// on subscribes to a backend event and returns its unsubscriber. React's
// StrictMode mounts effects twice, so every listener must be disposable or
// handlers stack up and fire repeatedly.
export function on(event: string, handler: (...data: any[]) => void): () => void {
  return EventsOn(event, handler);
}

export { EventsEmit };

/**
 * Folders dropped on the window. The second argument tells the runtime to fire
 * only for drops that land on an element carrying --wails-drop-target, which
 * the app shell and the volume list both set.
 */
export function onFileDrop(handler: (paths: string[]) => void): () => void {
  OnFileDrop((_x: number, _y: number, paths: string[]) => handler(paths ?? []), true);
  return () => OnFileDropOff();
}
