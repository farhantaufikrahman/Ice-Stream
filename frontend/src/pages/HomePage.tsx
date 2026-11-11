import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { listFeed, listSubscriptions, listVideos } from '../api/endpoints'
import type { Profile } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { VideoGrid } from '../components/VideoGrid'
import { usePagedVideos } from '../hooks/usePagedVideos'

/** Jumlah kanal di baris "Kanal yang kamu ikuti". */
const STRIP_COUNT = 24

/**
 * Beranda. Chip di atas grid mengikuti desain Home.dc.html. Backend belum
 * punya kategori, jadi chip-nya hanya "Semua" dan (setelah login)
 * "Mengikuti": video terbaru dari kanal yang kamu ikuti.
 */
export function HomePage() {
  const { status } = useAuth()
  const [params, setParams] = useSearchParams()
  const following = status === 'authenticated' && params.get('tab') === 'mengikuti'

  return (
    <>
      {status === 'authenticated' && (
        <div className="chips" role="tablist" aria-label="Pilih daftar video">
          <button type="button" role="tab" aria-selected={!following} className={following ? 'chip-tab' : 'chip-tab is-active'} onClick={() => setParams({})}>
            Semua
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={following}
            className={following ? 'chip-tab is-active' : 'chip-tab'}
            onClick={() => setParams({ tab: 'mengikuti' })}
          >
            Mengikuti
          </button>
        </div>
      )}
      {following ? <FollowingFeed /> : <LatestFeed />}
    </>
  )
}

function LatestFeed() {
  const feed = usePagedVideos('home', listVideos)
  return (
    <>
      <h1 className="page-title">Video terbaru</h1>
      <VideoGrid feed={feed} empty="Belum ada video. Jadilah yang pertama mengunggah!" />
    </>
  )
}

function FollowingFeed() {
  const feed = usePagedVideos('feed', listFeed)
  return (
    <>
      <SubscriptionStrip />
      <h1 className="page-title">Dari kanal yang kamu ikuti</h1>
      <VideoGrid feed={feed} empty="Belum ada video di sini. Buka halaman kanal yang kamu suka, lalu tekan Ikuti." />
    </>
  )
}

/** Baris avatar kanal yang diikuti (GET /me/subscriptions), menuju halaman kanal. */
function SubscriptionStrip() {
  const [channels, setChannels] = useState<Profile[] | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    listSubscriptions('', ctrl.signal)
      .then((page) => setChannels(page.channels.slice(0, STRIP_COUNT)))
      // Baris ini hanya pelengkap: jika gagal, cukup tidak ditampilkan.
      .catch(() => setChannels([]))
    return () => ctrl.abort()
  }, [])
  if (!channels || channels.length === 0) return null
  return (
    <section className="subs-strip" aria-label="Kanal yang kamu ikuti">
      {channels.map((c) => {
        const name = c.display_name || c.username
        return (
          <Link key={c.id} to={`/channel/${encodeURIComponent(c.username)}`} className="subs-item" title={name}>
            <Avatar name={name} url={c.avatar_url} size={56} />
            <span className="subs-name">{name}</span>
          </Link>
        )
      })}
    </section>
  )
}
