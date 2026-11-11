import { Link } from 'react-router'
import type { Video } from '../api/types'
import { formatCount, formatDuration, timeAgo } from '../lib/format'
import { Avatar } from './Avatar'
import { Thumbnail } from './Thumbnail'

/**
 * Kartu video di grid (beranda, pencarian), mengikuti desain "Beranda".
 * hideOwner: untuk halaman kanal (desain Channel.dc.html), semua video
 * milik kanal yang sama, jadi avatar dan nama diganti jumlah tayangan.
 */
export function VideoCard({ video, hideOwner = false }: { video: Video; hideOwner?: boolean }) {
  const ownerName = video.owner?.display_name || video.owner?.username || 'Channel'
  return (
    <Link to={`/watch/${video.id}`} className="video-card">
      <div className="video-card-thumb">
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        {!hideOwner && <span className="thumb-views">▶ {formatCount(video.view_count)}</span>}
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </div>
      <div className="video-card-body">
        {!hideOwner && <Avatar name={ownerName} url={video.owner?.avatar_url} size={34} />}
        <div className="video-card-text">
          <span className="video-card-title" title={video.title}>
            {video.title}
          </span>
          <span className="muted">
            {hideOwner ? `${formatCount(video.view_count)} tayangan` : ownerName} · {timeAgo(video.created_at)}
          </span>
        </div>
      </div>
    </Link>
  )
}

/** Versi kecil untuk kolom "Berikutnya" di halaman tonton. */
export function VideoRow({ video }: { video: Video }) {
  const ownerName = video.owner?.display_name || video.owner?.username || 'Channel'
  return (
    <Link to={`/watch/${video.id}`} className="video-row">
      <div className="video-row-thumb">
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </div>
      <div className="video-card-text">
        <span className="video-card-title video-row-title">{video.title}</span>
        <span className="muted small">{ownerName}</span>
        <span className="muted small">{formatCount(video.view_count)} tayangan</span>
      </div>
    </Link>
  )
}
