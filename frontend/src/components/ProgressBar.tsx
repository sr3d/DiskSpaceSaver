// A bar for work whose size may or may not be known.
//
// Indeterminate is a state of its own, not zero percent: a bar sitting at the
// far left looks like a scan that has not started, when in fact it is running
// hard and simply has nothing credible to divide by. So it sweeps instead,
// which says "working" without claiming a position.
interface Props {
  /** Completion in [0,1], or a negative number when it cannot be known. */
  fraction: number;
  /** Announced to screen readers; the bar itself carries no text. */
  label: string;
  className?: string;
}

export function ProgressBar({ fraction, label, className }: Props) {
  const known = fraction >= 0;
  const pct = Math.max(0, Math.min(1, fraction)) * 100;

  return (
    <div
      className={'pbar' + (known ? '' : ' pbar-unknown') + (className ? ' ' + className : '')}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      // Omitting valuenow is what tells a screen reader the value is unknown;
      // passing 0 would have it announce "0 percent" over and over.
      aria-valuenow={known ? Math.round(pct) : undefined}
      aria-valuetext={known ? `${Math.round(pct)}%` : 'working, size unknown'}
      title={known ? `${Math.round(pct)}%` : 'Still working — the total is not known yet'}
    >
      <div className="pbar-fill" style={known ? { width: `${pct}%` } : undefined} />
    </div>
  );
}
