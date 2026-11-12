import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router'
import { ApiError, errorMessage, isAbortError } from '../api/client'
import { getVideo, listVideos, recordView, saveWatchProgress, setLike } from '../api/endpoints'
import type { Danmaku, Profile, Video, VideoDetail } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ChannelCardHead } from '../components/channel/ChannelCardHead'
import { ErrorBox } from '../components/ErrorBox'
import { HlsPlayer } from '../components/HlsPlayer'
import { categoryLabel } from '../lib/categories'
import { formatCount, formatDate } from '../lib/format'
import { VideoRow } from '../components/VideoCard'
import { CommentSection } from '../components/watch/CommentSection'
import { DanmakuComposer } from '../components/watch/DanmakuComposer'
import { DanmakuLayer, type DanmakuLayerHandle } from '../components/watch/DanmakuLayer'
import { DanmakuList } from '../components/watch/DanmakuList'
import { PlaylistPanel } from '../components/watch/PlaylistPanel'
import { ReportDialog } from '../components/watch/ReportDialog'
import { SaveButton } from '../components/watch/SaveButton'
import { ShareDialog } from '../components/watch/ShareDialog'
import { useDanmaku } from '../hooks/useDanmaku'
import { usePlaylistQueue, type PlaylistQueue } from '../hooks/usePlaylistQueue'
import { watchInListPath } from '../lib/playlistQueue'
import { readDanmakuPref, saveDanmakuPref } from '../lib/danmakuPref'
import { parseStartParam } from '../lib/share'

/** Jumlah video di kolom "Berikutnya". */
const UP_NEXT_COUNT = 8

/** Pilihan "Putar otomatis" di panel playlist, juga diingat per browser. */
const AUTONEXT_PREF_KEY = 'ice-autonext'

function readAutoNextPref(): boolean {
  try {
    return localStorage.getItem(AUTONEXT_PREF_KEY) !== 'off'
  } catch {
    return true
  }
}

function saveAutoNextPref(on: boolean): void {
  try {
    localStorage.setItem(AUTONEXT_PREF_KEY, on ? 'on' : 'off')
  } catch {
    // Tidak tersimpan pun tidak apa-apa.
  }
}

/**
 * Dikirim lewat state navigasi saat pindah otomatis ke video berikutnya,
 * supaya video itu langsung diputar. Membuka link biasa tidak memutar
 * otomatis.
 */
interface WatchNavState {
  autoplay?: boolean
}

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
  const [params] = useSearchParams()
  const listId = params.get('list')
  const { status: authStatus, user } = useAuth()
  // Antrean playlist ada di sini (bukan di WatchView) supaya tidak dimuat
  // ulang setiap kali pindah ke video berikutnya.
  const queue = usePlaylistQueue(listId, id)
  const [autoNext, setAutoNext] = useState(readAutoNextPref)
  const changeAutoNext = (on: boolean) => {
    setAutoNext(on)
    saveAutoNextPref(on)
  }
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
  return (
    <WatchView
      key={detail.video.id}
      detail={detail}
      loggedIn={authStatus === 'authenticated'}
      user={user}
      playlist={listId && queue ? { id: listId, queue, autoNext, onAutoNextChange: changeAutoNext } : null}
    />
  )
}

interface PlaylistContext {
  id: string
  queue: PlaylistQueue
  autoNext: boolean
  onAutoNextChange: (on: boolean) => void
}

interface WatchViewProps {
  detail: VideoDetail
  loggedIn: boolean
  user: Profile | null
  /** Ada jika video dibuka dari playlist (?list=...). */
  playlist: PlaylistContext | null
}

function WatchView({ detail, loggedIn, user, playlist }: WatchViewProps) {
  const { video } = detail
  const location = useLocation()
  const navigate = useNavigate()
  const autoplay = (location.state as WatchNavState | null)?.autoplay === true
  const playable = video.status === 'READY' && Boolean(video.hls_url)
  const videoRef = useRef<HTMLVideoElement>(null)
  const layerRef = useRef<DanmakuLayerHandle>(null)
  const [showDanmaku, setShowDanmaku] = useState(readDanmakuPref)
  // Id danmaku yang masuk setelah halaman dibuka (dikirim sendiri atau
  // live). Set, karena danmaku sendiri datang dua kali: dari jawaban POST
  // dan dari siaran live. Jumlah di bawah judul = danmaku_count + ukuran Set.
  const [newDanmakuIds, setNewDanmakuIds] = useState<ReadonlySet<string>>(() => new Set())
  const countDanmaku = (d: Danmaku) => setNewDanmakuIds((ids) => (ids.has(d.id) ? ids : new Set(ids).add(d.id)))
  // Danmaku live dari penonton lain langsung ditampilkan di layar.
  const danmaku = useDanmaku(video.id, videoRef, playable, (d) => {
    layerRef.current?.show(d)
    countDanmaku(d)
  })

  const toggleDanmaku = () => {
    setShowDanmaku((on) => {
      saveDanmakuPref(!on)
      return !on
    })
  }
  const onDanmakuSent = (d: Danmaku) => {
    danmaku.add(d)
    layerRef.current?.show(d, true)
    countDanmaku(d)
  }
  const seekTo = (ms: number) => {
    const el = videoRef.current
    if (el) el.currentTime = ms / 1000
  }
  const [views, setViews] = useState(video.view_count)
  const [like, setLikeState] = useState({ liked: detail.viewer_liked ?? false, count: video.like_count })
  const [likeBusy, setLikeBusy] = useState(false)
  const [likeError, setLikeError] = useState<string | null>(null)

  // Server sudah memutuskan posisi lanjut (langkah 13): 0 jika baru
  // menonton kurang dari 5 detik atau tinggal kurang dari 10 detik lagi.
  // Tautan dari tombol Bagikan (?t=83) menang atas posisi terakhir
  // menonton. Waktu di luar panjang video diabaikan.
  const linkStart = parseStartParam(new URLSearchParams(location.search).get('t'))
  const fromLink = linkStart > 0 && (!video.duration_ms || linkStart * 1000 < video.duration_ms)
  const startAt = fromLink ? linkStart : (detail.resume_position_ms ?? 0) / 1000
  const [dialog, setDialog] = useState<null | { kind: 'share'; seconds: number } | { kind: 'report' }>(null)
  const isOwnVideo = user?.id === video.owner_id

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

  // Video selesai: lanjut ke video berikutnya di playlist jika Putar
  // otomatis aktif. Dibaca dari ref supaya HlsPlayer tidak perlu dibuat ulang.
  const nextPath = playlist?.autoNext && playlist.queue.next ? watchInListPath(playlist.queue.next.video.id, playlist.id) : null
  const nextRef = useRef(nextPath)
  useLayoutEffect(() => {
    nextRef.current = nextPath
  })
  const onEnded = useCallback(() => {
    if (nextRef.current) navigate(nextRef.current, { state: { autoplay: true } satisfies WatchNavState })
  }, [navigate])

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

  // Satu elemen dipasang di dua tempat; CSS menyembunyikan salah satunya
  // sesuai lebar halaman tonton.
  const playlistPanel = playlist && (
    <PlaylistPanel
      playlistId={playlist.id}
      queue={playlist.queue}
      autoNext={playlist.autoNext}
      onAutoNextChange={playlist.onAutoNextChange}
    />
  )

  return (
    <div className="watch">
      <div className="watch-main">
        <div className="player-card">
          {playable && video.hls_url ? (
            <HlsPlayer
              src={video.hls_url}
              poster={video.thumbnail_url}
              startAt={startAt}
              onFirstPlay={onFirstPlay}
              onProgress={onProgress}
              onEnded={onEnded}
              autoPlay={autoplay}
              videoRef={videoRef}
            >
              <DanmakuLayer ref={layerRef} videoRef={videoRef} items={danmaku.list} visible={showDanmaku} />
            </HlsPlayer>
          ) : (
            <div className="player player-placeholder">
              <p>{statusText[video.status] ?? 'Video belum bisa diputar.'}</p>
              {video.status === 'FAILED' && video.failure_reason && <p className="faint">{video.failure_reason}</p>}
            </div>
          )}
        </div>
        {playable && (
          <DanmakuComposer
            videoId={video.id}
            videoRef={videoRef}
            loggedIn={loggedIn}
            visible={showDanmaku}
            onToggle={toggleDanmaku}
            onSent={onDanmakuSent}
          />
        )}

        <div className="watch-heading">
          <h1 className="watch-title">{video.title}</h1>
          <div className="watch-stats">
            <span>{formatCount(views)} tayangan</span>
            <span>{formatCount(video.danmaku_count + newDanmakuIds.size)} danmaku</span>
            <span>Diunggah {formatDate(video.created_at)}</span>
            {startAt > 0 && !fromLink && <span className="accent">Melanjutkan dari posisi terakhir</span>}
          </div>
          <VideoTags category={video.category} tags={video.tags} />
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
          <SaveButton
            videoId={video.id}
            saved={{ watchLater: detail.viewer_saved ?? false, inPlaylist: detail.viewer_in_playlist ?? false }}
            loggedIn={loggedIn}
            disabled={video.status !== 'READY'}
          />
          <button
            type="button"
            className="btn btn-like"
            onClick={() => setDialog({ kind: 'share', seconds: videoRef.current?.currentTime ?? 0 })}
            disabled={video.status !== 'READY'}
            aria-haspopup="dialog"
          >
            <ShareIcon /> Bagikan
          </button>
          {/* Pemilik tidak bisa melaporkan videonya sendiri (server juga menolak). */}
          {!isOwnVideo &&
            video.status === 'READY' &&
            (loggedIn ? (
              <button type="button" className="btn btn-ghost-muted" onClick={() => setDialog({ kind: 'report' })} aria-haspopup="dialog">
                Laporkan
              </button>
            ) : (
              <Link to={loginLink} className="btn btn-ghost-muted" title="Masuk untuk melaporkan video">
                Laporkan
              </Link>
            ))}
        </div>
        {dialog?.kind === 'share' && (
          <ShareDialog target={{ videoId: video.id, currentSeconds: dialog.seconds }} title={video.title} onClose={() => setDialog(null)} />
        )}
        {dialog?.kind === 'report' && <ReportDialog videoId={video.id} title={video.title} onClose={() => setDialog(null)} />}
        {likeError && <ErrorBox message={likeError} />}

        <section className="channel-card">
          {video.owner ? (
            <ChannelCardHead owner={video.owner} />
          ) : (
            <>
              <Avatar name={ownerName} size={52} />
              <div className="channel-text">
                <span className="channel-name">{ownerName}</span>
              </div>
            </>
          )}
          <p className="description">{video.description || 'Tidak ada deskripsi.'}</p>
        </section>

        {/* Layar sempit: kolom kanan turun ke bawah komentar, jadi panel
            playlist ditampilkan di sini (lihat .watch-playlist-* di CSS). */}
        {playlistPanel && <div className="watch-playlist-inline">{playlistPanel}</div>}

        {playable && <CommentSection videoId={video.id} videoOwnerId={video.owner_id} initialCount={video.comment_count} user={user} />}
      </div>

      <aside className="watch-side">
        {playlistPanel && <div className="watch-playlist-side">{playlistPanel}</div>}
        {playable && <DanmakuList items={danmaku.list} onSeek={seekTo} />}
        <h2 className="side-title">Berikutnya</h2>
        <UpNext currentId={video.id} category={video.category} />
      </aside>
    </div>
  )
}

/**
 * Kategori dan tag di bawah judul (desain Watch.dc.html: "#kuliner #esKrim").
 * Kategori membuka chip kategori itu di beranda. Tag membuka pencarian
 * kata itu, seperti Bilibili; tag ikut disimpan sebagai kata pencarian di
 * backend, jadi video bertag #esKrim ditemukan dengan kata "eskrim".
 */
function VideoTags({ category, tags }: { category: string; tags: string[] }) {
  const label = categoryLabel(category)
  if (!label && tags.length === 0) return null
  return (
    <div className="watch-stats watch-tags">
      {label && (
        <Link className="watch-category" to={`/?${new URLSearchParams({ category })}`}>
          {label}
        </Link>
      )}
      {tags.map((t) => (
        <Link key={t} to={`/search?${new URLSearchParams({ q: t })}`}>
          #{t}
        </Link>
      ))}
    </div>
  )
}

/**
 * Video lain di kolom kanan halaman tonton: yang sekategori lebih dulu,
 * lalu ditambah video terbaru supaya daftarnya tetap penuh.
 */
function UpNext({ currentId, category }: { currentId: string; category: string }) {
  const [videos, setVideos] = useState<Video[] | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    const sameCategory = category ? listVideos('', ctrl.signal, category).catch(() => null) : Promise.resolve(null)
    Promise.all([sameCategory, listVideos('', ctrl.signal).catch(() => null)]).then(([same, latest]) => {
      if (ctrl.signal.aborted) return
      // Gabungkan tanpa dobel: video sekategori juga ada di daftar terbaru.
      const merged = new Map<string, Video>()
      for (const v of [...(same?.videos ?? []), ...(latest?.videos ?? [])]) {
        if (!merged.has(v.id)) merged.set(v.id, v)
      }
      setVideos([...merged.values()])
    })
    return () => ctrl.abort()
  }, [category])
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

function ShareIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="18" cy="5" r="3" />
      <circle cx="6" cy="12" r="3" />
      <circle cx="18" cy="19" r="3" />
      <line x1="8.6" y1="13.5" x2="15.4" y2="17.5" />
      <line x1="15.4" y1="6.5" x2="8.6" y2="10.5" />
    </svg>
  )
}
