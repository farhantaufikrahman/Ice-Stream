import { Link } from 'react-router'
import type { LiveStream } from '../../api/types'
import { categoryLabel } from '../../lib/categories'
import { formatCount } from '../../lib/format'
import { liveOwnerName, liveTone } from '../../lib/live'
import { LogoMark } from '../Logo'

/** Kartu siaran di grid "Sedang live", halaman Live, dan pencarian. */
export function LiveCard({ stream }: { stream: LiveStream }) {
  const category = categoryLabel(stream.category)
  return (
    <Link to={`/live/${stream.id}`} className="live-card">
      <div className="live-card-thumb" style={{ background: liveTone(stream.owner_id) }}>
        <LogoMark size={72} />
        <span className="live-badge">LIVE</span>
        {category && <span className="thumb-category">{category}</span>}
        <span className="live-watching">{formatCount(stream.viewer_count)} menonton</span>
      </div>
      <span className="video-card-title" title={stream.title}>
        {stream.title}
      </span>
      <span className="muted small">{liveOwnerName(stream)}</span>
    </Link>
  )
}

/** Grid kartu siaran. */
export function LiveGrid({ streams }: { streams: LiveStream[] }) {
  return (
    <div className="live-grid">
      {streams.map((s) => (
        <LiveCard key={s.id} stream={s} />
      ))}
    </div>
  )
}
