// One scan tab: chart, sidebar, breadcrumbs and the actions over them.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  api, ItemRef, NodeSummary, ScanSummary, ViewportSlice, PREF_DEFAULT,
  KIND_DIR, KIND_PACKAGE,
} from '../api';
import { Palette } from '../colors';
import { formatAge, formatBytes, formatCount, middleTruncate } from '../format';
import { Arc } from '../sunburst';
import { reportError, useStore } from '../store';
import { Breadcrumbs } from './Breadcrumbs';
import { Sidebar } from './Sidebar';
import { Sunburst } from './Sunburst';
import { ContextMenu, MenuItem } from './ContextMenu';

interface Props {
  scan: ScanSummary;
  palette: Palette;
  dark: boolean;
}

export function ScanView({ scan, palette, dark }: Props) {
  const prefs = useStore((s) => s.prefs);
  const tab = useStore((s) => s.tabs[scan.id]);
  const patchTab = useStore((s) => s.patchTab);
  const collector = useStore((s) => s.collector);
  const toast = useStore((s) => s.toast);
  const askConfirm = useStore((s) => s.askConfirm);
  const setActiveScan = useStore((s) => s.setActiveScan);
  const setScreen = useStore((s) => s.setScreen);

  const base2 = prefs?.base2 ?? false;
  const rings = prefs?.rings ?? 5;
  const showHidden = prefs?.showHidden ?? true;

  const focus = tab?.focus ?? 1;
  const [slice, setSlice] = useState<ViewportSlice | undefined>(undefined);
  const [rows, setRows] = useState<NodeSummary[]>([]);
  const [trail, setTrail] = useState<NodeSummary[]>([]);
  const [menu, setMenu] = useState<{ x: number; y: number; id: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const reqSeq = useRef(0);

  const collected = useMemo(() => {
    const s = new Set<number>();
    for (const it of collector.items) if (it.scanID === scan.id) s.add(it.nodeID);
    return s;
  }, [collector, scan.id]);

  // load pulls the viewport, the sidebar page and the breadcrumbs for a focus.
  const load = useCallback(
    async (nodeID: number) => {
      const seq = ++reqSeq.current;
      try {
        const [v, kids, anc] = await Promise.all([
          api.getViewport(scan.id, nodeID, rings, PREF_DEFAULT),
          api.getChildren(scan.id, nodeID, 0, 500),
          api.getAncestors(scan.id, nodeID),
        ]);
        if (seq !== reqSeq.current) return; // a newer navigation won
        setSlice(v);
        setRows(showHidden ? (kids ?? []) : (kids ?? []).filter((k) => (k.flags & 2) === 0));
        setTrail(anc ?? []);
        patchTab(scan.id, { loading: false });
      } catch (err) {
        if (seq === reqSeq.current) reportError('Could not read that folder', err);
      }
    },
    [scan.id, rings, showHidden, patchTab],
  );

  useEffect(() => {
    load(focus);
  }, [focus, load]);

  // While a scan runs the tree keeps changing under us. The snapshot event
  // carries the root viewport, which is only the right answer when the user is
  // looking at the root; otherwise it is a tick telling us to refetch.
  useEffect(() => {
    if (scan.status !== 'scanning') return;
    let alive = true;
    const unsub = [
      onEvent('scan:snapshot', (payload: any) => {
        if (!alive || payload?.scanID !== scan.id) return;
        if (focus === 1 && payload.viewport) {
          setSlice(payload.viewport);
          api.getChildren(scan.id, 1, 0, 500).then((k) => alive && setRows(k)).catch(() => {});
        } else {
          load(focus);
        }
      }),
      onEvent('scan:done', (s: ScanSummary) => {
        if (!alive || s.id !== scan.id) return;
        load(focus);
      }),
    ];
    return () => {
      alive = false;
      unsub.forEach((u) => u());
    };
  }, [scan.id, scan.status, focus, load]);

  useEffect(() => {
    const unsub = onEvent('tree:changed', (payload: any) => {
      if (payload?.scanID === scan.id) load(focus);
    });
    return unsub;
  }, [scan.id, focus, load]);

  // ---- actions -----------------------------------------------------------
  const ref = (id: number): ItemRef => ({ scanID: scan.id, nodeID: id });
  const selected = tab?.selected ?? 0;
  const selectedRow = rows.find((r) => r.id === selected);

  const navigate = (id: number) => {
    if (!id) return;
    patchTab(scan.id, { focus: id, selected: 0, hovered: 0, loading: true });
  };

  const zoomOut = () => {
    if (trail.length >= 2) navigate(trail[trail.length - 2].id);
    else setScreen('volumes');
  };

  const act = async (fn: () => Promise<unknown>, failMsg: string) => {
    setBusy(true);
    try {
      await fn();
    } catch (err) {
      reportError(failMsg, err);
    } finally {
      setBusy(false);
    }
  };

  const doTrash = (ids: number[]) => {
    if (ids.length === 0) return;
    const run = () =>
      act(async () => {
        const res = await api.moveToTrash(ids.map(ref));
        const failed = Object.keys(res.failed ?? {});
        if (res.succeeded?.length) {
          toast('success', `Moved ${res.succeeded.length} item${res.succeeded.length === 1 ? '' : 's'} to the Trash`,
            `${formatBytes(res.bytes, base2)} reclaimed`);
        }
        if (failed.length) {
          toast('error', `${failed.length} item${failed.length === 1 ? '' : 's'} could not be moved`,
            res.failed[failed[0]]);
        }
      }, 'Could not move to the Trash');

    if (prefs?.confirmTrash) {
      const names = ids.map((i) => rows.find((r) => r.id === i)?.name ?? 'item');
      askConfirm({
        title: ids.length === 1 ? `Move “${names[0]}” to the Trash?` : `Move ${ids.length} items to the Trash?`,
        body: 'You can put items back from the Trash in Finder until you empty it.',
        confirmLabel: 'Move to Trash',
        onConfirm: run,
      });
    } else run();
  };

  const doDeleteForever = (ids: number[]) => {
    if (ids.length === 0) return;
    const names = ids.map((i) => rows.find((r) => r.id === i)?.name ?? 'item');
    askConfirm({
      title: ids.length === 1 ? `Delete “${names[0]}” permanently?` : `Delete ${ids.length} items permanently?`,
      body: 'This bypasses the Trash. There is no way to get these back.',
      confirmLabel: 'Delete Permanently',
      destructive: true,
      onConfirm: () =>
        act(async () => {
          const res = await api.deletePermanently(ids.map(ref));
          if (res.succeeded?.length) {
            toast('success', `Deleted ${res.succeeded.length} item${res.succeeded.length === 1 ? '' : 's'}`,
              `${formatBytes(res.bytes, base2)} reclaimed`);
          }
          const failed = Object.keys(res.failed ?? {});
          if (failed.length) toast('error', `${failed.length} could not be deleted`, res.failed[failed[0]]);
        }, 'Could not delete'),
    });
  };

  // Expose the actions to the menu bar and global keyboard handler.
  useEffect(() => {
    const handlers: Record<string, () => void> = {
      reveal: () => selected && act(() => api.reveal([ref(selected)]), 'Could not show in Finder'),
      quickLook: () => selected && act(() => api.quickLook([ref(selected)], 0), 'Could not preview'),
      getInfo: () => selected && act(() => api.getInfo(ref(selected)), 'Could not open Get Info'),
      open: () => selected && act(() => api.openItem(ref(selected)), 'Could not open'),
      copyPath: () =>
        selected &&
        act(async () => {
          const p = await api.copyPath(ref(selected));
          toast('info', 'Path copied', middleTruncate(p, 64));
        }, 'Could not copy the path'),
      collect: () => selected && act(() => api.collectorAdd([ref(selected)]), 'Could not collect'),
      trash: () => doTrash(selected ? [selected] : []),
      deleteForever: () => doDeleteForever(selected ? [selected] : []),
      zoomIn: () => selectedRow?.hasKids && navigate(selected),
      zoomOut: () => zoomOut(),
      rescan: () =>
        act(async () => {
          const id = await api.rescan(scan.id, focus);
          if (id !== scan.id) setActiveScan(id);
          else toast('info', 'Rescanned', trail[trail.length - 1]?.name ?? '');
        }, 'Could not rescan'),
      selectAll: () => {},
    };
    (window as any).__dssActions = handlers;
    return () => {
      if ((window as any).__dssActions === handlers) delete (window as any).__dssActions;
    };
  });

  const menuItems = (id: number): MenuItem[] => {
    const row = rows.find((r) => r.id === id) ?? null;
    const isDir = row ? row.kind === KIND_DIR || row.kind === KIND_PACKAGE : false;
    return [
      { label: 'Show in Finder', onClick: () => act(() => api.reveal([ref(id)]), 'Could not show in Finder') },
      { label: 'Quick Look', onClick: () => act(() => api.quickLook([ref(id)], 0), 'Could not preview') },
      { label: 'Get Info', onClick: () => act(() => api.getInfo(ref(id)), 'Could not open Get Info') },
      { label: 'Open', onClick: () => act(() => api.openItem(ref(id)), 'Could not open') },
      { separator: true },
      {
        label: 'Copy Path',
        onClick: () =>
          act(async () => {
            const p = await api.copyPath(ref(id));
            toast('info', 'Path copied', middleTruncate(p, 64));
          }, 'Could not copy the path'),
      },
      ...(isDir ? [{ label: 'Zoom In', onClick: () => navigate(id) } as MenuItem] : []),
      {
        label: 'Rescan This Folder',
        onClick: () => act(() => api.rescan(scan.id, id), 'Could not rescan'),
      },
      { separator: true },
      {
        label: collected.has(id) ? 'Remove from Collector' : 'Add to Collector',
        onClick: () =>
          act(
            () => (collected.has(id) ? api.collectorRemove(scan.id, id) : api.collectorAdd([ref(id)])),
            'Could not update the collector',
          ),
      },
      { label: 'Move to Trash', onClick: () => doTrash([id]), destructive: true },
      ...(prefs?.allowPermanentDelete
        ? [{ label: 'Delete Permanently…', onClick: () => doDeleteForever([id]), destructive: true } as MenuItem]
        : []),
    ];
  };

  const rootTotal = slice?.rootTotal ?? scan.bytes;
  const focusTotal = slice?.focus.size ?? 0;

  return (
    <div className="scanview">
      <div className="scanbar">
        <Breadcrumbs trail={trail} base2={base2} onNavigate={navigate} onHome={() => setScreen('volumes')} />
        <div className="scanbar-right">
          {scan.status === 'scanning' ? (
            <>
              <span className="scan-progress" title={scan.currentPath}>
                {formatCount(scan.files)} files · {formatBytes(scan.bytes, base2)}
              </span>
              {scan.stalled && (
                <span className="scan-warn" title={scan.currentPath}>
                  waiting on {scan.currentPath ? middleTruncate(scan.currentPath, 40) : 'the disk'}
                </span>
              )}
              <button className="btn btn-quiet" onClick={() => api.cancelScan(scan.id)}>
                Stop
              </button>
            </>
          ) : (
            <>
              {scan.fromCache && scan.cachedAt && (
                <span className="scan-cached">saved scan · {formatAge(Date.now() - scan.cachedAt)}</span>
              )}
              {scan.inaccessible > 0 && (
                <span className="scan-warn" title="Grant Full Disk Access or scan as administrator">
                  {formatCount(scan.inaccessible)} unreadable
                </span>
              )}
              <button
                className="btn btn-quiet"
                onClick={() =>
                  act(async () => {
                    const id = await api.rescan(scan.id, 1);
                    if (id !== scan.id) setActiveScan(id);
                  }, 'Could not rescan')
                }
              >
                Rescan
              </button>
            </>
          )}
        </div>
      </div>

      <div className="scanbody">
        <Sunburst
          slice={slice}
          rootLabel={scan.label}
          palette={palette}
          dark={dark}
          base2={base2}
          rings={rings}
          hovered={tab?.hovered ?? 0}
          selected={selected}
          scanning={scan.status === 'scanning'}
          onHover={(arc: Arc | null) => patchTab(scan.id, { hovered: arc?.id ?? 0 })}
          onSelect={(arc) => arc.id !== 0 && patchTab(scan.id, { selected: arc.id })}
          onZoom={(arc) => navigate(arc.id)}
          onZoomOut={zoomOut}
          onContextMenu={(arc, x, y) => arc.id !== 0 && setMenu({ x, y, id: arc.id })}
        />

        <Sidebar
          rows={rows}
          total={focusTotal}
          palette={palette}
          dark={dark}
          base2={base2}
          hovered={tab?.hovered ?? 0}
          selected={selected}
          collected={collected}
          loading={tab?.loading ?? false}
          onHover={(id) => patchTab(scan.id, { hovered: id })}
          onSelect={(id) => patchTab(scan.id, { selected: id })}
          onOpen={navigate}
          onContextMenu={(id, x, y) => setMenu({ x, y, id })}
        />
      </div>

      <div className="scanfoot">
        <span>
          {formatBytes(focusTotal, base2)} here
          {rootTotal > 0 && focusTotal !== rootTotal && (
            <> · {formatBytes(rootTotal, base2)} in {scan.label}</>
          )}
        </span>
        {busy && <span className="foot-busy">working…</span>}
      </div>

      {menu && (
        <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.id)} onClose={() => setMenu(null)} />
      )}
    </div>
  );
}

// onEvent is a local import shim so this file does not reach into runtime
// directly; it keeps the subscribe/unsubscribe contract in one place.
import { on as onEvent } from '../api';
