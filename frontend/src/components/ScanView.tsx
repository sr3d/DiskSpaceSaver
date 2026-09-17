// One scan tab: chart, sidebar, breadcrumbs and the actions over them.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  api, ItemRef, NodeSummary, ScanSummary, SelectedItem, SubProgress, ViewportSlice,
  PREF_DEFAULT, KIND_DIR, KIND_PACKAGE,
} from '../api';
import { Palette } from '../colors';
import { formatAge, formatBytes, formatCount, middleTruncate } from '../format';
import { Arc } from '../sunburst';
import { reportError, useStore } from '../store';
import { Breadcrumbs } from './Breadcrumbs';
import { Sidebar } from './Sidebar';
import { Inspector } from './Inspector';
import { SizeFilter } from './SizeFilter';
import { SidebarColumn } from './SidebarColumn';
import { SelectionPane } from './SelectionPane';
import { SelectionColumn } from './SelectionColumn';
import { Sunburst } from './Sunburst';
import { ContextMenu, MenuItem } from './ContextMenu';
import { ProgressBar } from './ProgressBar';

interface Props {
  scan: ScanSummary;
  palette: Palette;
  dark: boolean;
}

export function ScanView({ scan, palette, dark }: Props) {
  const prefs = useStore((s) => s.prefs);
  const tab = useStore((s) => s.tabs[scan.id]);
  const patchTab = useStore((s) => s.patchTab);
  const navigateTo = useStore((s) => s.navigate);
  const goBack = useStore((s) => s.goBack);
  const goForward = useStore((s) => s.goForward);
  const setChecked = useStore((s) => s.setChecked);
  const layoutTier = useStore((s) => s.layout);
  const caps = useStore((s) => s.caps);
  const collector = useStore((s) => s.collector);
  const toast = useStore((s) => s.toast);
  const askConfirm = useStore((s) => s.askConfirm);
  const setActiveScan = useStore((s) => s.setActiveScan);
  const setScreen = useStore((s) => s.setScreen);

  const base2 = prefs?.base2 ?? false;
  const rings = prefs?.rings ?? 5;
  const uiScale = prefs?.uiScale ?? 1;
  const showHidden = prefs?.showHidden ?? true;
  const minItemSize = prefs?.minItemSize ?? 0;

  const focus = tab?.focus ?? 1;
  const [slice, setSlice] = useState<ViewportSlice | undefined>(undefined);
  const [rows, setRows] = useState<NodeSummary[]>([]);
  const [hidden, setHidden] = useState({ count: 0, bytes: 0 });
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
        const all = kids?.rows ?? [];
        setRows(showHidden ? all : all.filter((k) => (k.flags & 2) === 0));
        setHidden({ count: kids?.hidden ?? 0, bytes: kids?.hiddenBytes ?? 0 });
        setTrail(anc ?? []);
        patchTab(scan.id, { loading: false });
      } catch (err) {
        if (seq === reqSeq.current) reportError('Could not read that folder', err);
      }
    },
    // minItemSize is applied on the Go side, so a change to it has to make the
    // viewport and the list be fetched again — nothing here can filter after
    // the fact, and the "other" wedge is computed from it too.
    [scan.id, rings, showHidden, minItemSize, patchTab],
  );

  // Reloads whenever the focus moves and whenever the scan's lifecycle moves.
  //
  // Depending on the scan:done event alone was a race: a small folder finishes
  // in under a hundred milliseconds, before React has subscribed, and the view
  // then sat empty for good because the initial load had caught a tree with
  // nothing in it yet. Keying off the status makes the refresh happen whether
  // or not the event was heard.
  useEffect(() => {
    load(focus);
  }, [focus, load, scan.status, scan.finishedAt]);

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
          api
            .getChildren(scan.id, 1, 0, 500)
            .then((k) => {
              if (!alive) return;
              setRows(k?.rows ?? []);
              setHidden({ count: k?.hidden ?? 0, bytes: k?.hiddenBytes ?? 0 });
            })
            .catch(() => {});
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

  // A subtree rescan is a whole second walk with no scan record of its own, so
  // it reports separately. Without this the window sat still for as long as
  // the folder took, which is exactly the complaint "Open last scan" drew.
  const [rescanning, setRescanning] = useState<SubProgress | null>(null);
  useEffect(() => {
    const unsub = onEvent('scan:subprogress', (p: SubProgress) => {
      if (p?.scanID !== scan.id) return;
      setRescanning(p.done ? null : p);
    });
    return unsub;
  }, [scan.id]);

  // ---- actions -----------------------------------------------------------
  const ref = (id: number): ItemRef => ({ scanID: scan.id, nodeID: id });
  const selected = tab?.selected ?? 0;
  const selectedRow = rows.find((r) => r.id === selected);

  // ---- bulk selection ----------------------------------------------------
  const checked = tab?.checked ?? new Set<number>();

  // Ticks outlive the folder they were made in, so the selection cannot be
  // read off the visible rows any more — an item two folders back has no row
  // here at all. Go describes them instead, which is also what gives each one
  // the full path that makes a bulk delete reviewable.
  const [selItems, setSelItems] = useState<SelectedItem[]>([]);
  const checkedIDs = useMemo(
    () => [...checked].sort((a, b) => a - b),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [checked],
  );
  const checkedKey = checkedIDs.join(',');
  useEffect(() => {
    if (checkedIDs.length === 0) {
      setSelItems([]);
      return;
    }
    let alive = true;
    api
      .describeItems(checkedIDs.map((id) => ({ scanID: scan.id, nodeID: id })))
      .then((d) => alive && setSelItems(d ?? []))
      .catch(() => {});
    return () => {
      alive = false;
    };
    // `rows` is in here so a delete or a rescan re-describes: an item that has
    // gone is shown as gone rather than silently keeping its old size.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [checkedKey, scan.id, rows]);

  // What the selection is actually worth. A child whose parent is also ticked
  // contributes nothing — its bytes are inside the parent's — and an item that
  // has gone contributes nothing either.
  const selLive = selItems.filter((i) => !i.gone && !i.covered);
  const checkedBytes = selLive.reduce((a, i) => a + i.size, 0);
  const selFolders = new Set(selItems.filter((i) => !i.gone).map((i) => parentOf(i.path))).size;

  const toggleCheck = (id: number, shiftKey: boolean) => {
    const next = new Set(checked);
    const anchor = tab?.lastChecked ?? 0;
    if (shiftKey && anchor && anchor !== id) {
      // Shift extends from the last box ticked, the way every file list does.
      const a = rows.findIndex((r) => r.id === anchor);
      const b = rows.findIndex((r) => r.id === id);
      if (a >= 0 && b >= 0) {
        const [lo, hi] = a < b ? [a, b] : [b, a];
        const adding = !next.has(id);
        for (let i = lo; i <= hi; i++) {
          if (adding) next.add(rows[i].id);
          else next.delete(rows[i].id);
        }
        setChecked(scan.id, next, id);
        return;
      }
    }
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setChecked(scan.id, next, id);
  };

  const clearChecks = () => setChecked(scan.id, new Set<number>(), 0);

  const uncheck = (id: number) => {
    const next = new Set(checked);
    next.delete(id);
    setChecked(scan.id, next, 0);
  };

  // Drops the ticks for rows that have just been acted on. The result only
  // names paths, so which ones actually went cannot be told apart here; a tick
  // left behind on a row that survived costs the user one click, while a dead
  // id left in the set would sit there for the rest of the session.
  const dropChecks = (ids: number[]) => {
    if (!ids.some((id) => checked.has(id))) return;
    const next = new Set(checked);
    for (const id of ids) next.delete(id);
    setChecked(scan.id, next, 0);
  };

  const collectChecked = () =>
    act(async () => {
      await api.collectorAdd(selLive.map((i) => ref(i.nodeID)));
      clearChecks();
    }, 'Could not collect those items');

  // The pane is shown for as long as anything is ticked, wherever it lives.
  const selectionOpen = (tab?.selectionOpen ?? false) && checked.size > 0;

  // Opens the pane on the first tick, and puts the flag back when the last one
  // goes — deleting, collecting or unticking all empty the selection without
  // going near the flag, and a flag left set would let an Escape press be
  // swallowed by a pane that is not on screen.
  //
  // The edge is taken from the stored tick set rather than the visible rows,
  // because a tick can outlive the row it belongs to — the size filter or a
  // rescan can take the row away — and counting that as "none ticked" would
  // re-open a pane the user had closed. Seeded with the count at mount for the
  // same reason: coming back to a tab that already has ticks does not re-open
  // its pane.
  const tickCount = checked.size;
  const prevTicks = useRef(tickCount);
  useEffect(() => {
    const was = prevTicks.current;
    prevTicks.current = tickCount;
    if (tickCount > 0 && was === 0) patchTab(scan.id, { selectionOpen: true });
    else if (tickCount === 0 && (tab?.selectionOpen ?? false)) {
      patchTab(scan.id, { selectionOpen: false });
    }
  }, [tickCount, tab?.selectionOpen, scan.id, patchTab]);

  /**
   * What a delete or collect should act on: everything ticked, or the
   * highlighted row when nothing is. Covered items are left out — deleting the
   * folder they sit in takes them with it, and asking for both would report a
   * spurious failure for the child that vanished under us.
   */
  const bulkTargets = () =>
    checked.size > 0 ? selLive.map((i) => i.nodeID) : selected ? [selected] : [];

  const navigate = (id: number) => {
    if (!id) return;
    navigateTo(scan.id, id);
  };

  // Walks to where an item lives and highlights it there. A selected item may
  // be two folders away, and highlighting a handle the current list has no row
  // for would look like the click did nothing.
  const revealNode = (id: number) =>
    act(async () => {
      const line = await api.getAncestors(scan.id, id);
      const parent = line?.[line.length - 1];
      if (parent && parent.id !== (tab?.focus ?? 1)) navigateTo(scan.id, parent.id);
      patchTab(scan.id, { selected: id });
    }, 'Could not go to that item');

  // Up one level, which is a different thing from going back: it walks the
  // tree, not the trail.
  const zoomOut = () => {
    if (trail.length >= 2) navigate(trail[trail.length - 2].id);
    else setScreen('volumes');
  };

  const back = () => {
    if (!goBack(scan.id)) setScreen('volumes');
  };
  const forward = () => goForward(scan.id);

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

  // A confirmation has to name what it is about to delete, and the thing
  // being deleted may well not be on screen any more. The described selection
  // knows about those; the visible rows cover the case of acting on a
  // highlighted row that was never ticked.
  const nameOf = (id: number) =>
    selItems.find((i) => i.nodeID === id)?.name ?? rows.find((r) => r.id === id)?.name ?? 'item';
  const sizeOf = (id: number) =>
    selItems.find((i) => i.nodeID === id)?.size ?? rows.find((r) => r.id === id)?.size ?? 0;

  const doTrash = (ids: number[]) => {
    if (ids.length === 0) return;
    const run = () =>
      act(async () => {
        const res = await api.moveToTrash(ids.map(ref));
        dropChecks(ids);
        const failed = Object.keys(res.failed ?? {});
        if (res.succeeded?.length) {
          toast('success', `Moved ${res.succeeded.length} item${res.succeeded.length === 1 ? '' : 's'} to the ${caps.trashLabel}`,
            `${formatBytes(res.bytes, base2)} reclaimed`);
        }
        if (failed.length) {
          toast('error', `${failed.length} item${failed.length === 1 ? '' : 's'} could not be moved`,
            res.failed[failed[0]]);
        }
      }, `Could not move to the ${caps.trashLabel}`);

    if (prefs?.confirmTrash) {
      const names = ids.map((i) => nameOf(i));
      const bytes = ids.reduce((a, i) => a + sizeOf(i), 0);
      askConfirm({
        title:
          ids.length === 1
            ? `${caps.trashVerb}: “${names[0]}”?`
            : `${caps.trashVerb} for ${ids.length} items?`,
        body:
          (ids.length > 1 ? `${formatBytes(bytes, base2)} in total. ` : '') +
          `You can put items back from the ${caps.trashLabel} until you empty it.`,
        confirmLabel: caps.trashVerb,
        onConfirm: run,
      });
    } else run();
  };

  const doDeleteForever = (ids: number[]) => {
    if (ids.length === 0) return;
    const names = ids.map((i) => nameOf(i));
    const bytes = ids.reduce((a, i) => a + sizeOf(i), 0);
    askConfirm({
      title:
        ids.length === 1
          ? `Delete “${names[0]}” permanently?`
          : `Delete ${ids.length} items permanently?`,
      body:
        (ids.length > 1 ? `${formatBytes(bytes, base2)} in total. ` : '') +
        `This bypasses the ${caps.trashLabel}. There is no way to get these back.`,
      confirmLabel: 'Delete Permanently',
      destructive: true,
      onConfirm: () =>
        act(async () => {
          const res = await api.deletePermanently(ids.map(ref));
          dropChecks(ids);
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
      reveal: () => selected && act(() => api.reveal([ref(selected)]), `Could not ${caps.revealLabel.toLowerCase()}`),
      quickLook: () => selected && act(() => api.quickLook([ref(selected)], 0), 'Could not preview'),
      getInfo: () => selected && act(() => api.getInfo(ref(selected)), `Could not open ${caps.propertiesLabel}`),
      open: () => selected && act(() => api.openItem(ref(selected)), 'Could not open'),
      copyPath: () =>
        selected &&
        act(async () => {
          const p = await api.copyPath(ref(selected));
          toast('info', 'Path copied', middleTruncate(p, 64));
        }, 'Could not copy the path'),
      collect: () => {
        const ids = bulkTargets();
        if (ids.length) {
          act(async () => {
            await api.collectorAdd(ids.map(ref));
            clearChecks();
          }, 'Could not collect');
        }
      },
      trash: () => doTrash(bulkTargets()),
      deleteForever: () => doDeleteForever(bulkTargets()),
      selectAll: () => setChecked(scan.id, new Set(rows.map((r) => r.id)), rows[0]?.id ?? 0),
      zoomIn: () => selectedRow?.hasKids && navigate(selected),
      zoomOut: () => zoomOut(),
      back,
      forward,
      rescan: () =>
        act(async () => {
          const id = await api.rescan(scan.id, focus);
          if (id !== scan.id) setActiveScan(id);
          else {
            await load(focus);
            toast('info', 'Rescanned', trail[trail.length - 1]?.name ?? '');
          }
        }, 'Could not rescan'),
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
      { label: caps.revealLabel, onClick: () => act(() => api.reveal([ref(id)]), `Could not ${caps.revealLabel.toLowerCase()}`) },
      ...(caps.preview
        ? [{ label: 'Quick Look', onClick: () => act(() => api.quickLook([ref(id)], 0), 'Could not preview') } as MenuItem]
        : []),
      { label: caps.propertiesLabel, onClick: () => act(() => api.getInfo(ref(id)), `Could not open ${caps.propertiesLabel}`) },
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
      { label: caps.trashVerb, onClick: () => doTrash([id]), destructive: true },
      ...(prefs?.allowPermanentDelete
        ? [{ label: 'Delete Permanently…', onClick: () => doDeleteForever([id]), destructive: true } as MenuItem]
        : []),
    ];
  };

  const rootTotal = slice?.rootTotal ?? scan.bytes;
  const focusTotal = slice?.focus.size ?? 0;
  const selectedRowFlags = selectedRow?.flags ?? 0;
  const selectedFiles = selectedRow?.files ?? 0;

  // A third column only earns its place when the window can spare it.
  const inspectorInline = layoutTier !== 'wide';
  const inspector = (
    <Inspector
      scanID={scan.id}
      nodeID={selected}
      base2={base2}
      files={selectedFiles}
      flags={selectedRowFlags}
      inline={inspectorInline}
      onQuickLook={() => selected && act(() => api.quickLook([ref(selected)], 0), 'Could not preview')}
      onReveal={() => selected && act(() => api.reveal([ref(selected)]), `Could not ${caps.revealLabel.toLowerCase()}`)}
      onCopyPath={() =>
        selected &&
        act(async () => {
          const p = await api.copyPath(ref(selected));
          toast('info', 'Path copied', middleTruncate(p, 64));
        }, 'Could not copy the path')
      }
    />
  );

  return (
    <div className="scanview">
      <div className="scanbar">
        <div className="navbtns">
          <button
            className="icon-btn"
            onClick={back}
            disabled={(tab?.historyAt ?? 0) <= 0}
            title="Back (⌘[, Backspace or the mouse back button)"
            aria-label="Back"
          >
            ‹
          </button>
          <button
            className="icon-btn"
            onClick={forward}
            disabled={(tab?.historyAt ?? 0) >= ((tab?.history.length ?? 1) - 1)}
            title="Forward (⌘])"
            aria-label="Forward"
          >
            ›
          </button>
        </div>
        <Breadcrumbs trail={trail} base2={base2} onNavigate={navigate} onHome={() => setScreen('volumes')} />
        <div className="scanbar-right">
          {scan.status === 'scanning' ? (
            <>
              <ProgressBar
                fraction={scan.stalled ? -1 : scan.fraction}
                label={`Scanning ${scan.label}`}
                className="pbar-scan"
              />
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
          ) : rescanning ? (
            <>
              <ProgressBar
                fraction={rescanning.fraction}
                label="Rescanning this folder"
                className="pbar-scan"
              />
              <span className="scan-progress" title={rescanning.path}>
                rescanning · {formatCount(rescanning.files ?? 0)} files
              </span>
            </>
          ) : (
            <>
              {scan.fromCache && scan.cachedAt && (
                <span className="scan-cached">saved scan · {formatAge(Date.now() - scan.cachedAt)}</span>
              )}
              {scan.inaccessible > 0 && (
                <span className="scan-warn" title={caps.fullDiskAccess ? "Grant Full Disk Access or scan as administrator" : "Scan as administrator to include these"}>
                  {formatCount(scan.inaccessible)} unreadable
                </span>
              )}
              <button
                className="btn btn-quiet"
                title={
                  focus === 1
                    ? 'Rescan this whole scan'
                    : `Rescan ${trail[trail.length - 1]?.name ?? 'this folder'}`
                }
                onClick={() =>
                  act(async () => {
                    // Rescans where the user is looking, not the whole volume:
                    // they came here to re-check this folder.
                    const id = await api.rescan(scan.id, focus);
                    if (id !== scan.id) setActiveScan(id);
                    else await load(focus);
                  }, 'Could not rescan')
                }
              >
                {focus === 1 ? 'Rescan' : 'Rescan Folder'}
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
          uiScale={uiScale}
          hovered={tab?.hovered ?? 0}
          selected={selected}
          scanning={scan.status === 'scanning'}
          onHover={(arc: Arc | null) => patchTab(scan.id, { hovered: arc?.id ?? 0 })}
          onSelect={(arc) => arc.id !== 0 && patchTab(scan.id, { selected: arc.id })}
          onZoom={(arc) => navigate(arc.id)}
          onZoomOut={zoomOut}
          onContextMenu={(arc, x, y) => arc.id !== 0 && setMenu({ x, y, id: arc.id })}
        />

        <SidebarColumn scanID={scan.id} base2={base2} uiScale={uiScale}>
          {inspectorInline && inspector}
          <SizeFilter base2={base2} hidden={hidden} />
          <Sidebar
          rows={rows}
          checked={checked}
          onToggleCheck={toggleCheck}
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
        </SidebarColumn>
        {/* The third column: the inspector, the selection, or both stacked.
            Three columns and not four — the chart is what the app is for, and
            a fourth would come out of its width. */}
        {(selectionOpen || !inspectorInline) && (
          <SelectionColumn uiScale={uiScale} split={selectionOpen && !inspectorInline}>
            {!inspectorInline && inspector}
            {selectionOpen && (
              <SelectionPane
                items={selItems}
                indexOf={(id) => rows.findIndex((r) => r.id === id)}
                bytes={checkedBytes}
                folders={selFolders}
                base2={base2}
                palette={palette}
                dark={dark}
                allowPermanent={prefs?.allowPermanentDelete ?? false}
                hovered={tab?.hovered ?? 0}
                onHover={(id) => patchTab(scan.id, { hovered: id })}
                // Clicking an item from another folder should take the user
                // to it, not just highlight a handle the current list has no
                // row for.
                onSelect={(it) => {
                  patchTab(scan.id, { selected: it.nodeID });
                  if (!rows.some((r) => r.id === it.nodeID)) revealNode(it.nodeID);
                }}
                onContextMenu={(id, x, y) => setMenu({ x, y, id })}
                onRemove={uncheck}
                onCollect={collectChecked}
                onTrash={() => doTrash(bulkTargets())}
                onDeleteForever={() => doDeleteForever(bulkTargets())}
                onClear={clearChecks}
                onClose={() => patchTab(scan.id, { selectionOpen: false })}
              />
            )}
          </SelectionColumn>
        )}
      </div>

      <div className="scanfoot">
        <span className="foot-total">
          {formatBytes(focusTotal, base2)} here
          {rootTotal > 0 && focusTotal !== rootTotal && (
            <> · {formatBytes(rootTotal, base2)} in {scan.label}</>
          )}
        </span>
        {busy && <span className="foot-busy">working…</span>}
        {checked.size > 0 && (
          <div className="foot-sel">
            <button
              className={'sel-chip' + (selectionOpen ? ' sel-chip-on' : '')}
              onClick={() => patchTab(scan.id, { selectionOpen: !selectionOpen })}
              aria-expanded={selectionOpen}
              title={selectionOpen ? 'Hide the selected items' : 'Show the selected items again'}
            >
              <strong>{formatCount(checked.size)}</strong> selected ·{' '}
              {formatBytes(checkedBytes, base2)}
            </button>
            <button
              className="icon-btn"
              onClick={clearChecks}
              aria-label="Clear the selection"
              title="Clear the selection"
            >
              ×
            </button>
          </div>
        )}
      </div>

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          scale={uiScale}
          items={menuItems(menu.id)}
          onClose={() => setMenu(null)}
        />
      )}
    </div>
  );
}

// onEvent is a local import shim so this file does not reach into runtime
// directly; it keeps the subscribe/unsubscribe contract in one place.
import { on as onEvent } from '../api';

// The folder an item sits in, used only to count how many the selection spans.
function parentOf(path: string): string {
  const cut = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  return cut > 0 ? path.slice(0, cut) : path;
}
