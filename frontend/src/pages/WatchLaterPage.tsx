import { useState } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { listWatchLater, setWatchLater } from '../api/endpoints'
import type { WatchLaterEntry } from '../api/types'
import { ErrorBox } from '../components/ErrorBox'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedList } from '../hooks/usePagedList'
import { formatCount, formatDuration, timeAgo } from '../lib/format'

/**
 * Halaman Tonton nanti (/watch-later): video yang disimpan lewat tombol
 * Simpan di halaman tonton, yang terakhir disimpan di atas. Tampilannya
 * memakai baris yang sama dengan Riwayat tonton.
 */
export function WatchLaterPage() {
  const list = usePagedList<WatchLaterEntry>((token, signal) =>
    listWatchLater(token, signal).then((p) => ({ items: p.entries, next: p.next_page_token })),
  )
  const [error, setError] = useState<string | null>(null)

  const remove = async (videoId: string) => {
    setError(null)
    try {
      await setWatchLater(videoId, false)
      list.update((items) => items.filter((e) => e.video.id !== videoId))
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <div className="narrow-page">
      <div className="list-page-head">
        <h1 className="page-title">Tonton nanti</h1>
      </div>
      {error && <ErrorBox message={error} />}

      {list.items.length > 0 && (
        <ul className="history-list">
          {list.items.map((e) => (
            <li key={e.video.id}>
              <WatchLaterRow entry={e} onRemove={() => remove(e.video.id)} />
            </li>
          ))}
        </ul>
      )}
      {!list.loading && !list.error && list.items.length === 0 && (
        <div className="empty">
          <p>Belum ada video yang disimpan.</p>
          <p className="muted small">Tekan Simpan di bawah video untuk menontonnya nanti.</p>
          <Link to="/" className="btn">
            Cari video
          </Link>
        </div>
      )}
      {list.error && <ErrorBox message={list.error} onRetry={list.retry} />}
      {list.loading && <p className="muted center">Memuat…</p>}
      {!list.loading && !list.error && list.hasMore && (
        <div className="center">
          <button type="button" className="btn" onClick={list.loadMore}>
            Muat lagi
          </button>
        </div>
      )}
    </div>
  )
}

function WatchLaterRow({ entry, onRemove }: { entry: WatchLaterEntry; onRemove: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const { video } = entry
  const owner = video.owner?.display_name || video.owner?.username || 'Kanal'

  const handleRemove = async () => {
    setBusy(true)
    await onRemove()
    // Jika berhasil, baris ini hilang; jika gagal, tombolnya aktif lagi.
    setBusy(false)
  }

  return (
    <div className="history-row">
      <Link to={`/watch/${video.id}`} className="history-thumb" aria-label={video.title}>
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </Link>
      <div className="history-text">
        <Link to={`/watch/${video.id}`} className="history-title">
          {video.title}
        </Link>
        <span className="muted small">
          {owner} · {formatCount(video.view_count)} tayangan
        </span>
        <span className="muted small">Disimpan {timeAgo(entry.added_at)}</span>
      </div>
      <button
        type="button"
        className="icon-btn history-remove"
        onClick={handleRemove}
        disabled={busy}
        aria-label={`Keluarkan "${video.title}" dari Tonton nanti`}
        title="Keluarkan dari Tonton nanti"
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
    </div>
  )
}
