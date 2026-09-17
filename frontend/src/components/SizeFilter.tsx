// The size filter: a slider over round thresholds.
//
// It is a view filter, not a scan option. Hidden entries are still scanned and
// still counted in every total, which is why the bar says how much it is
// holding back rather than letting the numbers quietly stop adding up.
import { useEffect, useState } from 'react';
import { api } from '../api';
import { formatBytes, formatCount } from '../format';
import { reportError, useStore } from '../store';

// Round stops rather than a continuous scale: sizes span six orders of
// magnitude, so a linear slider would spend most of its travel between 4 and 5
// gigabytes, and nobody wants a threshold of 37.4 MB.
//
// The multiplier follows the unit the user reads, so every stop is labelled
// exactly — a binary stop shown in base 10 reads "10.5 MB", which looks like a
// mistake rather than a setting.
export function stops(base2: boolean): number[] {
  const k = base2 ? 1024 : 1000;
  const m = k * k;
  const g = m * k;
  return [0, 100 * k, 500 * k, 1 * m, 5 * m, 10 * m, 50 * m, 100 * m, 500 * m, 1 * g, 5 * g];
}

/** Nearest stop to a byte count, so a value set elsewhere still lands on the slider. */
export function stopIndex(list: number[], bytes: number): number {
  let best = 0;
  for (let i = 1; i < list.length; i++) {
    if (Math.abs(list[i] - bytes) < Math.abs(list[best] - bytes)) best = i;
  }
  return best;
}

/**
 * The digit shortcuts, as a ladder of decades rather than one key per stop.
 * Six keys cover the whole useful range and each is a round number the user
 * can hold in their head; nine keys onto every stop would be more reach for
 * less meaning, and "press 7 for 100 MB" is not a thing anyone remembers.
 *
 * Each rung is an existing stop, so the slider lands exactly on it.
 */
export function shortcutBytes(digit: number, base2: boolean): number | null {
  const k = base2 ? 1024 : 1000;
  const m = k * k;
  const g = m * k;
  const ladder = [0, 1 * m, 10 * m, 100 * m, 1 * g, 5 * g];
  return digit >= 0 && digit < ladder.length ? ladder[digit] : null;
}

interface Props {
  base2: boolean;
  hidden: { count: number; bytes: number };
  /** Rendered over the chart rather than stacked in the list column. */
  overlay?: boolean;
}

export function SizeFilter({ base2, hidden, overlay }: Props) {
  const prefs = useStore((s) => s.prefs);
  const setPrefs = useStore((s) => s.setPrefs);
  const stored = prefs?.minItemSize ?? 0;
  const STOPS = stops(base2);
  const [index, setIndex] = useState(() => stopIndex(STOPS, stored));

  // Follow the stored value when something else changes it, but not while this
  // slider is the thing changing it.
  useEffect(() => {
    setIndex(stopIndex(stops(base2), stored));
  }, [stored, base2]);

  const commit = async (i: number) => {
    try {
      setPrefs(await api.setMinItemSize(STOPS[i]));
    } catch (err) {
      reportError('Could not change the size filter', err);
    }
  };

  // The digit shortcuts live here rather than in the app's key handler: this
  // component owns the stops, and a second place that knew the ladder would be
  // a second place to get it wrong.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
      const st = useStore.getState();
      // A sheet is a question; changing what the view shows behind it is not
      // an answer.
      if (st.confirm || st.showPrefs || st.hiddenSpaceMount) return;
      if (!/^[0-9]$/.test(e.key)) return;
      const bytes = shortcutBytes(Number(e.key), base2);
      if (bytes === null) return;
      e.preventDefault();
      const i = stopIndex(stops(base2), bytes);
      setIndex(i);
      commit(i);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [base2]);

  const value = STOPS[index];
  return (
    <div
      className={'sizefilter' + (overlay ? ' sizefilter-overlay' : '')}
      title="Press 0–5 to step between sizes"
    >
      <div className="sizefilter-row">
        <label htmlFor="sizefilter-range">Show items over</label>
        <output className="sizefilter-value" htmlFor="sizefilter-range">
          {value === 0 ? 'any size' : formatBytes(value, base2)}
        </output>
      </div>
      <input
        id="sizefilter-range"
        type="range"
        min={0}
        max={STOPS.length - 1}
        step={1}
        value={index}
        // Dragging updates the label immediately; the scan is only re-queried
        // when the handle is let go, so a drag across the range does not fire
        // a query per stop.
        onChange={(e) => setIndex(Number(e.target.value))}
        onMouseUp={(e) => commit(Number((e.target as HTMLInputElement).value))}
        onKeyUp={(e) => commit(Number((e.target as HTMLInputElement).value))}
        onTouchEnd={(e) => commit(Number((e.target as HTMLInputElement).value))}
        aria-label="Hide items smaller than this"
        aria-valuetext={value === 0 ? 'any size' : formatBytes(value, base2)}
      />
      <div className="sizefilter-foot">
        <span className="sizefilter-scale" aria-hidden>
          all
        </span>
        {hidden.count > 0 ? (
          <span className="sizefilter-hidden" title="Still counted in every total">
            {formatCount(hidden.count)} hidden · {formatBytes(hidden.bytes, base2)}
          </span>
        ) : (
          <span className="sizefilter-hidden">showing everything here</span>
        )}
        <span className="sizefilter-scale" aria-hidden>
          {formatBytes(STOPS[STOPS.length - 1], base2)}
        </span>
      </div>
    </div>
  );
}
