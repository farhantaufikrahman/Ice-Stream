import { Link } from 'react-router'
import type { Playlist } from '../../api/types'
import { formatCount, timeAgo } from '../../lib/format'
import { Thumbnail } from '../Thumbnail'
import { LockIcon, PlaylistIcon } from './icons'

/**
 * Kartu playlist di tab Playlist halaman kanal dan hasil pencarian.
 * Bentuknya mengikuti kartu video. showOwner menampilkan nama pemilik
 * (di halaman kanal tidak perlu, pemiliknya sudah jelas).
 */
export function PlaylistCard({ playlist, showOwner = false }: { playlist: Playlist; showOwner?: boolean }) {
  const owner = playlist.owner?.display_name || playlist.owner?.username
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
        {showOwner && owner && <span className="muted">{owner}</span>}
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
