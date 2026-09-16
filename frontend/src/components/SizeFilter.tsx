// Hides entries below a size. It is a view filter, not a scan option: hidden
// entries are still counted in every total, which is why the bar says how much
// it is holding back rather than letting numbers quietly stop adding up.
import { api } from '../api';
import { formatBytes, formatCount } from '../format';
import { reportError, useStore } from '../store';

const STEPS: Array<[number, string]> = [
  [0, 'Everything'],
  [1024 * 1024, '1 MB'],
  [10 * 1024 * 1024, '10 MB'],
  [100 * 1024 * 1024, '100 MB'],
  [1024 * 1024 * 1024, '1 GB'],
];

interface Props {
  base2: boolean;
  hidden: { count: number; bytes: number };
}

export function SizeFilter({ base2, hidden }: Props) {
  const prefs = useStore((s) => s.prefs);
  const setPrefs = useStore((s) => s.setPrefs);
  const min = prefs?.minItemSize ?? 0;

  const set = async (bytes: number) => {
    try {
      setPrefs(await api.setMinItemSize(bytes));
    } catch (err) {
      reportError('Could not change the size filter', err);
    }
  };

  return (
    <div className="sizefilter">
      <label>
        <span>Hide under</span>
        <select value={String(min)} onChange={(e) => set(Number(e.target.value))}>
          {STEPS.map(([bytes, label]) => (
            <option key={bytes} value={bytes}>
              {label}
            </option>
          ))}
          {/* A value set elsewhere, from Preferences or an older file, still
              needs to show as the current selection. */}
          {!STEPS.some(([b]) => b === min) && (
            <option value={min}>{formatBytes(min, base2)}</option>
          )}
        </select>
      </label>
      {hidden.count > 0 && (
        <span className="sizefilter-hidden" title="Still counted in every total">
          {formatCount(hidden.count)} hidden · {formatBytes(hidden.bytes, base2)}
        </span>
      )}
    </div>
  );
}
