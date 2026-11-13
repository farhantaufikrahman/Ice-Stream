import { useState } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { clearHistory, deleteHistoryEntry, listHistory } from '../api/endpoints'
import type { HistoryEntry } from '../api/types'
import { ErrorBox } from '../components/ErrorBox'
import { Modal } from '../components/Modal'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedList } from '../hooks/usePagedList'
import { formatDuration, timeAgo } from '../lib/format'
import { watchedFraction } from '../lib/history'

/**
 * Halaman Riwayat tonton (/history). Posisi terakhir disimpan player setiap
 * 15 detik dan saat dijeda (HlsPlayer), jadi membuka video dari sini melanjutkan
 * dari posisi itu.
 */
export function HistoryPage() {
  const list = usePagedList<HistoryEntry>((token, signal) =>
    listHistory(token, signal).then((p) => ({ items: p.entries, next: p.next_page_token })),
  )
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const remove = async (videoId: string) => {
    setError(null)
    try {
      await deleteHistoryEntry(videoId)
      list.update((items) => items.filter((e) => e.video.id !== videoId))
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <div className="narrow-page">
      <div className="list-page-head">
        <h1 className="page-title">Riwayat tonton</h1>
        <button type="button" className="btn btn-small" onClick={() => setConfirming(true)} disabled={list.items.length === 0}>
          Hapus semua riwayat
        </button>
      </div>
      {error && <ErrorBox message={error} />}

      {list.items.length > 0 && (
        <ul className="history-list">
          {list.items.map((e) => (
            <li key={e.video.id}>
              <HistoryRow entry={e} onRemove={() => remove(e.video.id)} />
            </li>
          ))}
        </ul>
      )}
      {!list.loading && !list.error && list.items.length === 0 && (
        <div className="empty">
          <p>Belum ada video di riwayat.</p>
          <Link to="/" className="btn">
            Cari video untuk ditonton
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

      {confirming && <ClearHistoryDialog onClose={() => setConfirming(false)} onCleared={() => list.update(() => [])} />}
    </div>
  )
}

function HistoryRow({ entry, onRemove }: { entry: HistoryEntry; onRemove: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const { video, position_ms } = entry
  const owner = video.owner?.display_name || video.owner?.username || 'Kanal'
  const fraction = watchedFraction(position_ms, video.duration_ms)
  const progress =
    video.duration_ms && position_ms > 0 ? `Terakhir di ${formatDuration(position_ms)} dari ${formatDuration(video.duration_ms)}` : null

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
        {fraction > 0 && (
          <span className="progress-track" aria-hidden="true">
            <span className="progress-fill" style={{ width: `${fraction * 100}%` }} />
          </span>
        )}
      </Link>
      <div className="history-text">
        <Link to={`/watch/${video.id}`} className="history-title">
          {video.title}
        </Link>
        <span className="muted small">{owner}</span>
        <span className="muted small">
          Ditonton {timeAgo(entry.watched_at)}
          {progress && ` · ${progress}`}
        </span>
      </div>
      <button type="button" className="icon-btn history-remove" onClick={handleRemove} disabled={busy} aria-label={`Hapus "${video.title}" dari riwayat`} title="Hapus dari riwayat">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
    </div>
  )
}

function ClearHistoryDialog({ onClose, onCleared }: { onClose: () => void; onCleared: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const confirm = async () => {
    setBusy(true)
    setError(null)
    try {
      await clearHistory()
      onCleared()
      onClose()
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }
  return (
    <Modal title="Hapus semua riwayat?" onClose={onClose} locked={busy}>
      <div className="modal-body">
        <p>Semua video di riwayat dan posisi terakhirnya akan dihapus. Video yang dibuka lagi akan diputar dari awal.</p>
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="button" className="btn btn-danger" onClick={confirm} disabled={busy}>
            {busy ? 'Menghapus…' : 'Hapus semua'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
