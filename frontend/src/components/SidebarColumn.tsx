// The list column: the generic resizable column, wired to its own preference.
import { ReactNode, useCallback } from 'react';
import { api } from '../api';
import { useStore } from '../store';
import { ResizableColumn } from './ResizableColumn';

const MIN = 220;
const MAX = 900;
const DEFAULT = 350;

interface Props {
  scanID: string;
  base2: boolean;
  uiScale: number;
  children: ReactNode;
}

export function SidebarColumn({ uiScale, children }: Props) {
  const stored = useStore((s) => s.prefs?.sidebarWidth ?? DEFAULT);
  const setPrefs = useStore((s) => s.setPrefs);

  const commit = useCallback(
    (w: number) => {
      api.setSidebarWidth(w).then(setPrefs).catch(() => {});
    },
    [setPrefs],
  );

  return (
    <ResizableColumn
      className="sidecol"
      width={stored}
      min={MIN}
      max={MAX}
      fallback={DEFAULT}
      uiScale={uiScale}
      onCommit={commit}
      // Below this the decorative bar column costs more than it gives: the
      // name is what the user is reading, and the percentage and size still
      // carry the proportion. Driven from the measured width rather than a
      // container query, which the WebView on early macOS 13 does not support.
      tightBelow={430}
      label="the list"
    >
      {children}
    </ResizableColumn>
  );
}
