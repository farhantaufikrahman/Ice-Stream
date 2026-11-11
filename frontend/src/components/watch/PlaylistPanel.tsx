import { useEffect, useRef } from 'react'
import { Link } from 'react-router'
import type { PlaylistQueue } from '../../hooks/usePlaylistQueue'
import { formatDuration } from '../../lib/format'
import { watchInListPath } from '../../lib/playlistQueue'
import { ErrorBox } from '../ErrorBox'
import { LockIcon } from '../playlist/icons'
import { Thumbnail } from '../Thumbnail'

interface Props {
  playlistId: string
  queue: PlaylistQueue
  autoNext: boolean
  onAutoNextChange: (on: boolean) => void
}

/**
 * Panel playlist di kolom kanan halaman tonton (/watch/:id?list=...),
 * seperti panel playlist di YouTube: judul, posisi "3 / 12", daftar video
 * dengan video yang sedang diputar ditandai, dan pilihan Putar otomatis.
 */
export function PlaylistPanel({ playlistId, queue, autoNext, onAutoNextChange }: Props) {
  const { playlist, entries, index } = queue
  const currentRef = useRef<HTMLLIElement>(null)

  // Video yang sedang diputar digulir supaya terlihat di dalam panel
  // (hanya di dalam daftar, halaman tidak ikut bergulir).
  useEffect(() => {
    const el = currentRef.current
    const list = el?.parentElement
    if (el && list) list.scrollTop = el.offsetTop - list.offsetTop - 8
  }, [index])

  if (queue.error) {
    return (
      <section className="playlist-panel">
        <ErrorBox message={`Playlist tidak bisa dibuka: ${queue.error}`} onRetry={queue.retry} />
      </section>
    )
  }
  if (!playlist) {
    return (
      <section className="playlist-panel">
        <p className="muted small">Memuat playlist…</p>
      </section>
    )
  }

  const owner = playlist.owner?.display_name || playlist.owner?.username
  return (
    <section className="playlist-panel" aria-label={`Playlist ${playlist.title}`}>
      <header className="playlist-panel-head">
        <Link to={`/playlist/${playlist.id}`} className="playlist-panel-title">
          {playlist.title}
        </Link>
        <span className="muted small">
          {playlist.visibility === 'PRIVATE' && (
            <>
              <LockIcon /> Privat ·{' '}
            </>
          )}
          {owner && `${owner} · `}
          {index >= 0 ? `${index + 1} / ${playlist.video_count}` : `${playlist.video_count} video`}
        </span>
        <label className="toggle-row">
          <input type="checkbox" checked={autoNext} onChange={(e) => onAutoNextChange(e.target.checked)} />
          <span>Putar otomatis</span>
        </label>
      </header>
      {index === -1 && !queue.loading && !queue.needMore && <p className="muted small">Video ini tidak ada di playlist ini.</p>}
      <ol className="playlist-panel-list">
        {entries.map((e, i) => {
          const isCurrent = i === index
          return (
            <li key={e.video.id} ref={isCurrent ? currentRef : undefined}>
              <Link
                to={watchInListPath(e.video.id, playlistId)}
                className={isCurrent ? 'playlist-panel-row is-current' : 'playlist-panel-row'}
                aria-current={isCurrent ? 'true' : undefined}
              >
                <span className="playlist-panel-index" aria-hidden="true">
                  {isCurrent ? '▶' : i + 1}
                </span>
                <span className="playlist-panel-thumb">
                  <Thumbnail url={e.video.thumbnail_url} title={e.video.title} />
                  {e.video.duration_ms ? <span className="duration">{formatDuration(e.video.duration_ms)}</span> : null}
                </span>
                <span className="playlist-panel-text">
                  <span className="video-card-title video-row-title">{e.video.title}</span>
                  <span className="muted small">{e.video.owner?.display_name || e.video.owner?.username}</span>
                </span>
              </Link>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
