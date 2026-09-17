// Size and date formatting. Base 10 by default so numbers match Finder.

const B10 = ['kB', 'MB', 'GB', 'TB', 'PB'];
const B2 = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

export function formatBytes(bytes: number, base2 = false): string {
  const neg = bytes < 0;
  let b = Math.abs(bytes);
  const unit = base2 ? 1024 : 1000;
  const suffix = base2 ? B2 : B10;
  if (b < unit) return `${neg ? '-' : ''}${Math.round(b)} B`;
  let exp = -1;
  while (b >= unit && exp < suffix.length - 1) {
    b /= unit;
    exp++;
  }
  // One decimal below 100, none above: "9.4 GB" but "412 GB", which is how
  // Finder reads and keeps column widths stable.
  const digits = b < 10 ? 1 : b < 100 ? 1 : 0;
  return `${neg ? '-' : ''}${b.toFixed(digits)} ${suffix[exp]}`;
}

export function formatCount(n: number): string {
  return n.toLocaleString();
}

export function formatPercent(fraction: number): string {
  if (!isFinite(fraction) || fraction <= 0) return '0%';
  if (fraction < 0.001) return '<0.1%';
  if (fraction < 0.1) return `${(fraction * 100).toFixed(1)}%`;
  return `${Math.round(fraction * 100)}%`;
}

export function formatAge(millis: number): string {
  const s = Math.floor(millis / 1000);
  if (s < 60) return 'just now';
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} minute${m === 1 ? '' : 's'} ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} hour${h === 1 ? '' : 's'} ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d} day${d === 1 ? '' : 's'} ago`;
  const mo = Math.floor(d / 30);
  return `${mo} month${mo === 1 ? '' : 's'} ago`;
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${Math.round(s - m * 60)}s`;
}

// formatSnapshotDate turns tmutil's "2026-09-14-091500" into something legible.
export function formatSnapshotDate(raw: string): string {
  const m = raw.match(/^(\d{4})-(\d{2})-(\d{2})-(\d{2})(\d{2})(\d{2})$/);
  if (!m) return raw;
  const [, y, mo, d, h, mi] = m;
  const date = new Date(+y, +mo - 1, +d, +h, +mi);
  return date.toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  });
}

// middleTruncate keeps both ends of a long path readable.
export function middleTruncate(s: string, max: number): string {
  if (s.length <= max) return s;
  const keep = Math.floor((max - 1) / 2);
  return `${s.slice(0, keep)}…${s.slice(s.length - keep)}`;
}
