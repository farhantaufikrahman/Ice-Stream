import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ApiError, errorMessage, isAbortError } from '../api/client'
import { getPlaylist, listPlaylistVideos, setPlaylistVideo } from '../api/endpoints'
import type { Playlist, PlaylistEntry } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { DeletePlaylistDialog } from '../components/playlist/DeletePlaylistDialog'
import { LockIcon, PlaylistIcon } from '../components/playlist/icons'
import { PlaylistFormDialog } from '../components/playlist/PlaylistFormDialog'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedList } from '../hooks/usePagedList'
import { formatCount, formatDuration, timeAgo } from '../lib/format'
import { watchInListPath } from '../lib/playlistQueue'

interface Loaded {
  key: string
  playlist?: Playlist
  error?: { message: string; notFound: boolean }
}

/**
 * Halaman playlist /playlist/:id: kepala playlist (thumbnail video pertama,
 * judul, pemilik, deskripsi) lalu daftar videonya sesuai urutan
 * ditambahkan. Pemilik bisa mengedit, menghapus, dan mengeluarkan video.
 *
 * Desain Ice-Stream belum punya halaman playlist, jadi tampilannya memakai
 * bagian yang sudah ada: baris video dari Riwayat tonton dan dialog dari
 * Creator Studio.
 */
export function PlaylistPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { status, user } = useAuth()
  const [reloads, setReloads] = useState(0)
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [dialog, setDialog] = useState<'edit' | 'delete' | null>(null)
  // Video pertama di daftar, untuk tombol "Putar semua".
  const [firstVideoId, setFirstVideoId] = useState<string | null>(null)
  // Sama seperti useChannel: tunggu pengecekan login selesai supaya
  // playlist PRIVATE milik sendiri ikut membawa access token.
  const key = `${id}|${user?.id ?? ''}|${reloads}`

  useEffect(() => {
    if (status === 'loading') return
    const ctrl = new AbortController()
    getPlaylist(id, ctrl.signal)
      .then((playlist) => setLoaded({ key, playlist }))
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        const notFound = err instanceof ApiError && err.status === 404
        setLoaded({ key, error: { message: errorMessage(err), notFound } })
      })
    return () => ctrl.abort()
  }, [id, key, status])

  const setPlaylist = useCallback((fn: (p: Playlist) => Playlist) => {
    setLoaded((prev) => (prev?.playlist ? { ...prev, playlist: fn(prev.playlist) } : prev))
  }, [])

  const current = status !== 'loading' && loaded?.key === key ? loaded : null
  if (current?.error?.notFound) {
    return (
      <div className="center">
        <h1 className="page-title">Playlist tidak ditemukan</h1>
        <p className="muted">Playlist ini tidak ada, sudah dihapus, atau bersifat privat.</p>
        <Link to="/" className="btn">
          Kembali ke beranda
        </Link>
      </div>
    )
  }
  if (current?.error) return <ErrorBox message={current.error.message} onRetry={() => setReloads((n) => n + 1)} />
  if (!current?.playlist) return <p className="muted center">Memuat…</p>

  const playlist = current.playlist
  const isOwn = Boolean(user && user.id === playlist.owner_id)

  return (
    <div className="narrow-page playlist-page">
      <PlaylistHead
        playlist={playlist}
        isOwn={isOwn}
        firstVideoId={firstVideoId}
        onEdit={() => setDialog('edit')}
        onDelete={() => setDialog('delete')}
      />
      <PlaylistVideos
        key={key}
        playlistId={playlist.id}
        isOwn={isOwn}
        onFirstChange={setFirstVideoId}
        onRemoved={() => setPlaylist((p) => ({ ...p, video_count: Math.max(0, p.video_count - 1) }))}
      />

      {dialog === 'edit' && (
        <PlaylistFormDialog
          playlist={playlist}
          onClose={() => setDialog(null)}
          onSaved={(p) => {
            // Jawaban PATCH tidak membawa owner; pakai yang sudah ada.
            setPlaylist((old) => ({ ...p, owner: old.owner }))
            setDialog(null)
          }}
        />
      )}
      {dialog === 'delete' && (
        <DeletePlaylistDialog
          playlist={playlist}
          onClose={() => setDialog(null)}
          onDeleted={() => navigate(user ? `/channel/${encodeURIComponent(user.username)}?tab=playlist` : '/', { replace: true })}
        />
      )}
    </div>
  )
}

interface HeadProps {
  playlist: Playlist
  isOwn: boolean
  firstVideoId: string | null
  onEdit: () => void
  onDelete: () => void
}

function PlaylistHead({ playlist, isOwn, firstVideoId, onEdit, onDelete }: HeadProps) {
  const owner = playlist.owner
  const ownerName = owner?.display_name || owner?.username || 'Kanal'
  return (
    <section className="playlist-head">
      <div className="playlist-head-thumb">
        <Thumbnail url={playlist.thumbnail_url} title={playlist.title} />
        <span className="playlist-count">
          <PlaylistIcon /> {formatCount(playlist.video_count)} video
        </span>
      </div>
      <div className="playlist-head-text">
        <span className="playlist-kicker">
          {playlist.visibility === 'PRIVATE' ? (
            <span className="chip">
              <LockIcon /> Privat
            </span>
          ) : (
            <span className="chip chip-mint">Publik</span>
          )}
        </span>
        <h1 className="playlist-title">{playlist.title}</h1>
        {owner && (
          <Link to={`/channel/${encodeURIComponent(owner.username)}`} className="playlist-owner">
            <Avatar name={ownerName} url={owner.avatar_url} size={28} />
            <span>{ownerName}</span>
          </Link>
        )}
        <span className="muted small">
          {formatCount(playlist.video_count)} video · Diperbarui {timeAgo(playlist.updated_at)}
        </span>
        {playlist.description && <p className="description">{playlist.description}</p>}
        <div className="row-actions playlist-actions">
          {firstVideoId && (
            <Link to={watchInListPath(firstVideoId, playlist.id)} className="btn btn-primary btn-small">
              <PlayIcon /> Putar semua
            </Link>
          )}
          {isOwn && (
            <>
              <button type="button" className="btn btn-small" onClick={onEdit}>
                Edit
              </button>
              <button type="button" className="btn btn-small btn-danger-ghost" onClick={onDelete}>
                Hapus
              </button>
            </>
          )}
        </div>
      </div>
    </section>
  )
}

interface VideosProps {
  playlistId: string
  isOwn: boolean
  /** Melaporkan video pertama (null jika kosong) untuk tombol Putar semua. */
  onFirstChange: (videoId: string | null) => void
  onRemoved: () => void
}

function PlaylistVideos({ playlistId, isOwn, onFirstChange, onRemoved }: VideosProps) {
  const list = usePagedList<PlaylistEntry>((token, signal) =>
    listPlaylistVideos(playlistId, token, signal).then((p) => ({ items: p.entries, next: p.next_page_token })),
  )
  const [error, setError] = useState<string | null>(null)
  const firstId = list.items[0]?.video.id ?? null
  useEffect(() => {
    onFirstChange(firstId)
  }, [firstId, onFirstChange])

  const remove = async (videoId: string) => {
    setError(null)
    try {
      await setPlaylistVideo(playlistId, videoId, false)
      list.update((items) => items.filter((e) => e.video.id !== videoId))
      onRemoved()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <section className="playlist-videos">
      {error && <ErrorBox message={error} />}
      {list.items.length > 0 && (
        <ol className="history-list">
          {list.items.map((e, i) => (
            <li key={e.video.id}>
              <PlaylistRow
                index={i + 1}
                entry={e}
                href={watchInListPath(e.video.id, playlistId)}
                onRemove={isOwn ? () => remove(e.video.id) : undefined}
              />
            </li>
          ))}
        </ol>
      )}
      {!list.loading && !list.error && list.items.length === 0 && (
        <div className="empty">
          <p>Playlist ini belum berisi video.</p>
          {isOwn && <p className="muted small">Tekan Simpan di bawah video untuk menambahkannya ke playlist ini.</p>}
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
    </section>
  )
}

interface RowProps {
  index: number
  entry: PlaylistEntry
  /** Membuka video di dalam playlist (/watch/:id?list=...). */
  href: string
  onRemove?: () => Promise<void>
}

function PlaylistRow({ index, entry, href, onRemove }: RowProps) {
  const [busy, setBusy] = useState(false)
  const { video } = entry
  const owner = video.owner?.display_name || video.owner?.username || 'Kanal'

  const handleRemove = async () => {
    if (!onRemove) return
    setBusy(true)
    await onRemove()
    // Jika berhasil, baris ini hilang; jika gagal, tombolnya aktif lagi.
    setBusy(false)
  }

  return (
    <div className="history-row">
      <span className="playlist-index" aria-hidden="true">
        {index}
      </span>
      <Link to={href} className="history-thumb" aria-label={video.title}>
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </Link>
      <div className="history-text">
        <Link to={href} className="history-title">
          {video.title}
        </Link>
        <span className="muted small">
          {owner} · {formatCount(video.view_count)} tayangan
        </span>
        <span className="muted small">Ditambahkan {timeAgo(entry.added_at)}</span>
      </div>
      {onRemove && (
        <button
          type="button"
          className="icon-btn history-remove"
          onClick={handleRemove}
          disabled={busy}
          aria-label={`Keluarkan "${video.title}" dari playlist`}
          title="Keluarkan dari playlist"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M18 6 6 18M6 6l12 12" />
          </svg>
        </button>
      )}
    </div>
  )
}

function PlayIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M7 4.5v15l13-7.5z" />
    </svg>
  )
}
