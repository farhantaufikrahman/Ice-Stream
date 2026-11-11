import { Link } from 'react-router'
import type { Playlist } from '../../api/types'
import { formatCount, timeAgo } from '../../lib/format'
import { Thumbnail } from '../Thumbnail'
import { LockIcon, PlaylistIcon } from './icons'

/** Kartu playlist di tab Playlist halaman kanal. Bentuknya mengikuti kartu video. */
export function PlaylistCard({ playlist }: { playlist: Playlist }) {
  return (
    <Link to={`/playlist/${playlist.id}`} className="video-card">
      <div className="video-card-thumb playlist-card-thumb">
        <Thumbnail url={playlist.thumbnail_url} title={playlist.title} />
        <span className="playlist-count">
          <PlaylistIcon /> {formatCount(playlist.video_count)} video
        </span>
      </div>
      <div className="video-card-text">
        <span className="video-card-title" title={playlist.title}>
          {playlist.title}
        </span>
        <span className="muted">
          {playlist.visibility === 'PRIVATE' && (
            <>
              <LockIcon /> Privat ·{' '}
            </>
          )}
          Diperbarui {timeAgo(playlist.updated_at)}
        </span>
      </div>
    </Link>
  )
}
