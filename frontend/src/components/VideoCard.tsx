import { Link } from 'react-router'
import type { Video } from '../api/types'
import { formatCount, formatDuration, timeAgo } from '../lib/format'
import { Avatar } from './Avatar'
import { Thumbnail } from './Thumbnail'

export function VideoCard({ video }: { video: Video }) {
  const ownerName = video.owner?.display_name || video.owner?.username || 'Channel'
  return (
    <article className="video-card">
      <Link to={`/watch/${video.id}`} className="video-card-thumb">
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </Link>
      <div className="video-card-body">
        <Avatar name={ownerName} url={video.owner?.avatar_url} size={32} />
        <div className="video-card-text">
          <Link to={`/watch/${video.id}`} className="video-card-title" title={video.title}>
            {video.title}
          </Link>
          <div className="muted">{ownerName}</div>
          <div className="muted">
            {formatCount(video.view_count)} x ditonton · {timeAgo(video.created_at)}
          </div>
        </div>
      </div>
    </article>
  )
}
