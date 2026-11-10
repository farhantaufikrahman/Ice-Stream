import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { ApiError, errorMessage, isAbortError } from '../api/client'
import { getVideo, listVideos, recordView, saveWatchProgress, setLike } from '../api/endpoints'
import type { Video, VideoDetail } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { HlsPlayer } from '../components/HlsPlayer'
import { formatCount, formatDate } from '../lib/format'
import { VideoRow } from '../components/VideoCard'

/** Jumlah video di kolom "Berikutnya". */
const UP_NEXT_COUNT = 8

const statusText: Record<string, string> = {
  PENDING_UPLOAD: 'File video belum selesai diunggah.',
  UPLOADED: 'Video sedang menunggu diproses.',
  PROCESSING: 'Video sedang diproses (transcoding). Coba buka lagi sebentar.',
  FAILED: 'Video gagal diproses.',
}

interface LoadResult {
  key: string
  detail?: VideoDetail
  error?: { message: string; notFound: boolean }
}

export function WatchPage() {
  const { id = '' } = useParams()
  const { status: authStatus, user } = useAuth()
  const [reloads, setReloads] = useState(0)
  // Hasil disimpan bersama key-nya. Jika key sudah berganti (pindah video,
  // login/logout), hasil lama diabaikan dan halaman tampil "Memuat…".
  const [result, setResult] = useState<LoadResult | null>(null)

  // Tunggu pengecekan login selesai, supaya request pertama sudah membawa
  // access token (viewer_liked dan resume_position_ms ikut dijawab, dan
  // pemilik bisa membuka videonya yang belum READY). Muat ulang saat user
  // login/logout.
  const key = `${id}|${user?.id ?? ''}|${reloads}`
  useEffect(() => {
    if (authStatus === 'loading') return
    const ctrl = new AbortController()
    getVideo(id, ctrl.signal)
      .then((detail) => setResult({ key, detail }))
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        const notFound = err instanceof ApiError && err.status === 404
        setResult({ key, error: { message: errorMessage(err), notFound } })
      })
    return () => ctrl.abort()
  }, [id, key, authStatus])

  const current = authStatus !== 'loading' && result?.key === key ? result : null
  const error = current?.error
  const detail = current?.detail

  if (error) {
    return error.notFound ? (
      <div className="center">
        <h1 className="page-title">Video tidak ditemukan</h1>
        <Link to="/" className="btn">
          Kembali ke beranda
        </Link>
      </div>
    ) : (
      <ErrorBox message={error.message} onRetry={() => setReloads((n) => n + 1)} />
    )
  }
  if (!detail) {
    return <p className="muted center">Memuat…</p>
  }
  // key: state di dalam (jumlah like, dan lain-lain) diulang dari awal saat
  // pindah ke video lain.
  return <WatchView key={detail.video.id} detail={detail} loggedIn={authStatus === 'authenticated'} />
}

function WatchView({ detail, loggedIn }: { detail: VideoDetail; loggedIn: boolean }) {
  const { video } = detail
  const location = useLocation()
  const [views, setViews] = useState(video.view_count)
  const [like, setLikeState] = useState({ liked: detail.viewer_liked ?? false, count: video.like_count })
  const [likeBusy, setLikeBusy] = useState(false)
  const [likeError, setLikeError] = useState<string | null>(null)

  // Server sudah memutuskan posisi lanjut (langkah 13): 0 jika baru
  // menonton kurang dari 5 detik atau tinggal kurang dari 10 detik lagi.
  const startAt = (detail.resume_position_ms ?? 0) / 1000

  const onFirstPlay = useCallback(() => {
    // Server menghitung satu view per penonton per 30 menit (langkah 10).
    recordView(video.id)
      .then((r) => setViews(r.view_count))
      .catch(() => {
        // Gagal mencatat view tidak boleh mengganggu menonton.
      })
  }, [video.id])

  const onProgress = useCallback(
    (seconds: number) => {
      if (!loggedIn) return
      // keepalive: request tetap terkirim walau user menutup tab.
      saveWatchProgress(video.id, seconds * 1000, true).catch(() => {})
    },
    [video.id, loggedIn],
  )

  const toggleLike = async () => {
    const want = !like.liked
    const before = like
    // Optimistic update: tampilan langsung berubah, lalu dibetulkan
    // dengan jawaban server (atau dikembalikan jika gagal).
    setLikeState({ liked: want, count: like.count + (want ? 1 : -1) })
    setLikeBusy(true)
    setLikeError(null)
    try {
      const r = await setLike(video.id, want)
      setLikeState({ liked: r.liked, count: r.like_count })
    } catch (err) {
      setLikeState(before)
      setLikeError(errorMessage(err))
    } finally {
      setLikeBusy(false)
    }
  }

  const ownerName = video.owner?.display_name || video.owner?.username || 'Channel'
  const loginLink = `/login?next=${encodeURIComponent(location.pathname)}`
  const likeLabel = like.liked ? 'Batal suka' : 'Suka'

  return (
    <div className="watch">
      <div className="watch-main">
        <div className="player-card">
          {video.status === 'READY' && video.hls_url ? (
            <HlsPlayer
              src={video.hls_url}
              poster={video.thumbnail_url}
              startAt={startAt}
              onFirstPlay={onFirstPlay}
              onProgress={onProgress}
            />
          ) : (
            <div className="player player-placeholder">
              <p>{statusText[video.status] ?? 'Video belum bisa diputar.'}</p>
              {video.status === 'FAILED' && video.failure_reason && <p className="faint">{video.failure_reason}</p>}
            </div>
          )}
        </div>

        <div className="watch-heading">
          <h1 className="watch-title">{video.title}</h1>
          <div className="watch-stats">
            <span>{formatCount(views)} tayangan</span>
            <span>Diunggah {formatDate(video.created_at)}</span>
            {startAt > 0 && <span className="accent">Melanjutkan dari posisi terakhir</span>}
          </div>
        </div>

        <div className="watch-actions">
          {loggedIn ? (
            <button
              type="button"
              className={like.liked ? 'btn btn-like is-liked' : 'btn btn-like'}
              onClick={toggleLike}
              disabled={likeBusy || video.status !== 'READY'}
              aria-pressed={like.liked}
              aria-label={`${likeLabel}, ${like.count} suka`}
            >
              <HeartIcon filled={like.liked} /> {formatCount(like.count)}
            </button>
          ) : (
            <Link to={loginLink} className="btn btn-like" title="Masuk untuk menyukai video">
              <HeartIcon filled={false} /> {formatCount(like.count)}
            </Link>
          )}
        </div>
        {likeError && <ErrorBox message={likeError} />}

        <section className="channel-card">
          <Avatar name={ownerName} url={video.owner?.avatar_url} size={52} />
          <div className="channel-text">
            <span className="channel-name">{ownerName}</span>
            {video.owner && <span className="muted small">@{video.owner.username}</span>}
          </div>
          <p className="description">{video.description || 'Tidak ada deskripsi.'}</p>
        </section>
      </div>

      <aside className="watch-side">
        <h2 className="side-title">Berikutnya</h2>
        <UpNext currentId={video.id} />
      </aside>
    </div>
  )
}

/** Daftar video terbaru lainnya, di kolom kanan halaman tonton. */
function UpNext({ currentId }: { currentId: string }) {
  const [videos, setVideos] = useState<Video[] | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    listVideos('', ctrl.signal)
      .then((page) => setVideos(page.videos))
      .catch(() => setVideos([]))
    return () => ctrl.abort()
  }, [])
  if (!videos) return <p className="muted">Memuat…</p>
  const others = videos.filter((v) => v.id !== currentId).slice(0, UP_NEXT_COUNT)
  if (others.length === 0) return <p className="muted">Belum ada video lain.</p>
  return (
    <div className="up-next">
      {others.map((v) => (
        <VideoRow key={v.id} video={v} />
      ))}
    </div>
  )
}

function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z" />
    </svg>
  )
}
