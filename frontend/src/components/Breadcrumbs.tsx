import { NodeSummary } from '../api';
import { formatBytes } from '../format';

interface Props {
  trail: NodeSummary[];
  base2: boolean;
  onNavigate(id: number): void;
  onHome(): void;
}

export function Breadcrumbs({ trail, base2, onNavigate, onHome }: Props) {
  return (
    <nav className="crumbs" aria-label="Location">
      <button className="crumb crumb-home" onClick={onHome} title="Back to volumes">
        Volumes
      </button>
      {trail.map((c, i) => (
        <span key={c.id} className="crumb-group">
          <span className="crumb-sep" aria-hidden>›</span>
          <button
            className={'crumb' + (i === trail.length - 1 ? ' crumb-current' : '')}
            onClick={() => onNavigate(c.id)}
            aria-current={i === trail.length - 1 ? 'location' : undefined}
            title={`${c.name} — ${formatBytes(c.size, base2)}`}
          >
            {c.name}
          </button>
        </span>
      ))}
    </nav>
  );
}
