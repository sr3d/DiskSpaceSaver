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

interface Props {
  base2: boolean;
  hidden: { count: number; bytes: number };
}

export function SizeFilter({ base2, hidden }: Props) {
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

  const value = STOPS[index];
  return (
    <div className="sizefilter">
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
