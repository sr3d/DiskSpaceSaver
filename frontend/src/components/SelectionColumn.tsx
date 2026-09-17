// The third column: the inspector, the selected items, or both stacked.
//
// They share one column rather than taking one each because the chart is what
// the app is for, and a fourth column comes out of the chart's width. They are
// also used together — pick something in the list, look at it, tick it — so
// putting them side by side is what the user is actually doing.
import { ReactNode, useCallback } from 'react';
import { api } from '../api';
import { useStore } from '../store';
import { ResizableColumn } from './ResizableColumn';

const MIN = 220;
const MAX = 900;
const DEFAULT = 330;

interface Props {
  uiScale: number;
  /** Whether the selection is sharing the column with the inspector. */
  split: boolean;
  children: ReactNode;
}

export function SelectionColumn({ uiScale, split, children }: Props) {
  const stored = useStore((s) => s.prefs?.selectionWidth ?? DEFAULT);
  const setPrefs = useStore((s) => s.setPrefs);

  const commit = useCallback(
    (w: number) => {
      api.setSelectionWidth(w).then(setPrefs).catch(() => {});
    },
    [setPrefs],
  );

  return (
    <ResizableColumn
      className={'selcol' + (split ? ' selcol-split' : '')}
      width={stored}
      min={MIN}
      max={MAX}
      fallback={DEFAULT}
      uiScale={uiScale}
      onCommit={commit}
      label="the selection"
    >
      {children}
    </ResizableColumn>
  );
}
