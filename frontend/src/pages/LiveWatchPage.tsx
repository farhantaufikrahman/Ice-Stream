import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import { ApiError, errorMessage, isAbortError } from '../api/client'
import { getLive, listLive } from '../api/endpoints'
import type { LiveChatMessage, LiveStream } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ChannelCardHead } from '../components/channel/ChannelCardHead'
import { ErrorBox } from '../components/ErrorBox'
import { HlsPlayer } from '../components/HlsPlayer'
import { LiveChatPanel } from '../components/live/LiveChatPanel'
import { LiveGrid } from '../components/live/LiveCard'
import { DanmakuLayer, type DanmakuLayerHandle } from '../components/watch/DanmakuLayer'
import { ShareDialog } from '../components/watch/ShareDialog'
import { useLiveChat } from '../hooks/useLiveChat'
import { categoryLabel } from '../lib/categories'
import { readDanmakuPref, saveDanmakuPref } from '../lib/danmakuPref'
import { formatCount } from '../lib/format'
import { chatAsDanmaku, liveElapsed, liveOwnerName, liveStartClock } from '../lib/live'
import { formatClock } from '../lib/danmaku'
import { NotFoundPage } from './NotFoundPage'

/**
 * Jumlah penonton dan status (LIVE/ENDED) diperbarui server setiap 5 detik
 * dari MediaMTX; halaman membacanya ulang setiap 10 detik.
 */
const REFRESH_MS = 10_000
/** Kartu "Live lainnya" di bawah (desain Live.dc.html). */
const OTHERS_COUNT = 4

/** /live/:id. key={id} supaya pindah siaran memulai semuanya dari awal. */
export function LiveWatchRoute() {
  const { id = '' } = useParams()
  return <LiveWatchPage key={id} id={id} />
}

/**
 * Halaman tonton siaran langsung (langkah 25c, desain Live.dc.html):
 * player HLS dengan badge LIVE, lama siaran, dan jumlah penonton; chat
 * langsung di kanan (setiap pesan juga melayang sebagai danmaku); lalu
 * "Live lainnya".
 */
function LiveWatchPage({ id }: { id: string }) {
  const { user } = useAuth()
  const [stream, setStream] = useState<LiveStream | null>(null)
  const [error, setError] = useState<{ message: string; notFound: boolean } | null>(null)
  const [reloads, setReloads] = useState(0)

  useEffect(() => {
    const ctrl = new AbortController()
    const load = () =>
      getLive(id, ctrl.signal)
        .then((s) => {
          setStream(s)
          setError(null)
        })
        .catch((err: unknown) => {
          if (isAbortError(err)) return
          // Gagal saat memperbarui: tetap tampilkan data terakhir.
          setStream((cur) => {
            if (!cur) setError({ message: errorMessage(err), notFound: err instanceof ApiError && err.status === 404 })
            return cur
          })
        })
    void load()
    const timer = setInterval(load, REFRESH_MS)
    return () => {
      ctrl.abort()
      clearInterval(timer)
    }
  }, [id, reloads])

  if (error?.notFound) return <NotFoundPage />
  if (error) return <ErrorBox message={error.message} onRetry={() => setReloads((n) => n + 1)} />
  if (!stream) return <p className="muted center">Memuat…</p>
  return <LiveView stream={stream} viewerId={user?.id ?? ''} />
}

function LiveView({ stream, viewerId }: { stream: LiveStream; viewerId: string }) {
  const live = stream.status === 'LIVE' && Boolean(stream.playback_url)
  const videoRef = useRef<HTMLVideoElement>(null)
  const layerRef = useRef<DanmakuLayerHandle>(null)
  const [danmakuOn, setDanmakuOn] = useState(readDanmakuPref)
  const [sharing, setSharing] = useState(false)
  const showDanmaku = useCallback((m: LiveChatMessage, mine = false) => layerRef.current?.show(chatAsDanmaku(m), mine), [])
  const chat = useLiveChat(stream.id, live, (m) => showDanmaku(m, m.user?.id === viewerId))
  const category = categoryLabel(stream.category)
  const isOwn = viewerId !== '' && viewerId === stream.owner_id

  const toggleDanmaku = () => {
    setDanmakuOn((on) => {
      saveDanmakuPref(!on)
      return !on
    })
  }
  const onSent = (m: LiveChatMessage) => {
    chat.add(m)
    showDanmaku(m, true)
  }

  return (
    <div className="live-page">
      <div className="live-top">
        <div className="live-main">
          <div className="player-card">
            {live ? (
              <HlsPlayer src={stream.playback_url ?? ''} videoRef={videoRef} autoPlay>
                <DanmakuLayer ref={layerRef} videoRef={videoRef} items={[]} visible={danmakuOn} />
                <LiveBadges stream={stream} />
              </HlsPlayer>
            ) : (
              <div className="player player-placeholder">
                <p className="live-ended-title">Siaran sudah selesai</p>
                {stream.ended_at && (
                  <p className="muted small">
                    Berlangsung {formatClock(new Date(stream.ended_at).getTime() - new Date(stream.started_at).getTime())} · puncak{' '}
                    {formatCount(stream.peak_viewers)} penonton
                  </p>
                )}
              </div>
            )}
          </div>

          <div className="watch-heading">
            <h1 className="watch-title">{stream.title}</h1>
            <div className="watch-stats">
              {category && (
                <Link className="watch-category" to={`/live?${new URLSearchParams({ category: stream.category })}`}>
                  {category}
                </Link>
              )}
              <span>Mulai {liveStartClock(stream.started_at)}</span>
              {live && <span>Puncak {formatCount(stream.peak_viewers)} penonton</span>}
            </div>
          </div>

          <section className="channel-card live-channel-card">
            {stream.owner ? <ChannelCardHead owner={stream.owner} /> : <span className="channel-name">{liveOwnerName(stream)}</span>}
            <div className="watch-actions">
              {live && (
                <button type="button" className={danmakuOn ? 'btn dm-toggle is-on' : 'btn dm-toggle'} onClick={toggleDanmaku} aria-pressed={danmakuOn}>
                  {danmakuOn ? 'Danmaku aktif' : 'Danmaku mati'}
                </button>
              )}
              <button type="button" className="btn" onClick={() => setSharing(true)} aria-haspopup="dialog">
                Bagikan
              </button>
              {isOwn && (
                <Link to="/studio/live" className="btn">
                  Kelola siaran
                </Link>
              )}
            </div>
          </section>
          {sharing && <ShareDialog target={{ liveId: stream.id }} title={stream.title} onClose={() => setSharing(false)} />}
        </div>

        <LiveChatPanel
          streamId={stream.id}
          ownerId={stream.owner_id}
          viewerId={viewerId}
          live={live}
          messages={chat.messages}
          loaded={chat.loaded}
          onSent={onSent}
        />
      </div>

      <OtherLives currentId={stream.id} category={stream.category} />
    </div>
  )
}

/** Badge di pojok kiri atas player: LIVE, lama siaran, jumlah penonton. */
function LiveBadges({ stream }: { stream: LiveStream }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return (
    <div className="live-badges">
      <span className="live-badge">LIVE</span>
      <span className="live-pill mono">{liveElapsed(stream.started_at, now)}</span>
      <span className="live-pill">{formatCount(stream.viewer_count)} menonton</span>
    </div>
  )
}

/** "Live lainnya": siaran lain yang sekategori lebih dulu, lalu yang terbaru. */
function OtherLives({ currentId, category }: { currentId: string; category: string }) {
  const [streams, setStreams] = useState<LiveStream[]>([])
  useEffect(() => {
    const ctrl = new AbortController()
    const same = category ? listLive({ limit: OTHERS_COUNT + 1, category }, ctrl.signal).catch(() => []) : Promise.resolve([])
    Promise.all([same, listLive({ limit: OTHERS_COUNT + 1 }, ctrl.signal).catch(() => [])]).then(([a, b]) => {
      if (ctrl.signal.aborted) return
      const merged = new Map<string, LiveStream>()
      for (const s of [...a, ...b]) if (s.id !== currentId && !merged.has(s.id)) merged.set(s.id, s)
      setStreams([...merged.values()].slice(0, OTHERS_COUNT))
    })
    return () => ctrl.abort()
  }, [currentId, category])
  if (streams.length === 0) return null
  return (
    <section className="live-section" aria-labelledby="other-lives-title">
      <h2 id="other-lives-title" className="section-title">
        Live lainnya
      </h2>
      <LiveGrid streams={streams} />
    </section>
  )
}
