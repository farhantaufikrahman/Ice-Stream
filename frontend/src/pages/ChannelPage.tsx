import { Link, useParams, useSearchParams } from 'react-router'
import { listChannelVideos } from '../api/endpoints'
import type { Profile, Video } from '../api/types'
import { Avatar } from '../components/Avatar'
import { SubscribeButton } from '../components/channel/SubscribeButton'
import { ErrorBox } from '../components/ErrorBox'
import { Thumbnail } from '../components/Thumbnail'
import { VideoGrid } from '../components/VideoGrid'
import { useChannel } from '../hooks/useChannel'
import { usePagedVideos } from '../hooks/usePagedVideos'
import { formatCount, formatDate, formatDuration, timeAgo } from '../lib/format'

type Tab = 'video' | 'tentang'

const TABS: { id: Tab; label: string }[] = [
  { id: 'video', label: 'Video' },
  { id: 'tentang', label: 'Tentang' },
]

/**
 * Halaman kanal /channel/:username (desain Channel.dc.html).
 *
 * Bagian desain yang belum punya data di backend sengaja tidak
 * ditampilkan: jumlah video, total tayangan, total suka, tombol Kirim
 * pesan, tab Playlist dan Live, urutan Terpopuler, dan "Video unggulan"
 * (diganti video terbaru).
 */
export function ChannelPage() {
  const { username = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const tab: Tab = params.get('tab') === 'tentang' ? 'tentang' : 'video'
  const { channel, loading, error, isOwn, retry, applySubscription } = useChannel(username)

  if (error?.notFound) {
    return (
      <div className="center">
        <h1 className="page-title">Kanal tidak ditemukan</h1>
        <p className="muted">Tidak ada pengguna dengan nama @{username}.</p>
        <Link to="/" className="btn">
          Kembali ke beranda
        </Link>
      </div>
    )
  }
  if (error) return <ErrorBox message={error.message} onRetry={retry} />
  if (loading || !channel) return <p className="muted center">Memuat…</p>

  const user = channel.user
  const name = user.display_name || user.username
  const pickTab = (t: Tab) => setParams(t === 'video' ? {} : { tab: t }, { replace: true })

  return (
    <div className="channel-page">
      <ChannelBanner />
      <section className="channel-head">
        <span className="channel-avatar">
          <Avatar name={name} url={user.avatar_url} size={116} />
        </span>
        <div className="channel-info">
          <h1 className="channel-title">{name}</h1>
          <span className="muted">
            @{user.username}
            {user.bio && ` · ${firstLine(user.bio)}`}
          </span>
          <div className="channel-stats">
            <span>
              <strong>{formatCount(user.subscriber_count)}</strong> <span className="muted">pengikut</span>
            </span>
            <span className="muted">Bergabung {formatDate(user.created_at)}</span>
          </div>
        </div>
        <SubscribeButton
          username={user.username}
          subscribed={channel.viewer_subscribed ?? false}
          subscriberCount={user.subscriber_count}
          isOwn={isOwn}
          onChange={applySubscription}
        />
      </section>

      <div className="tabs" role="tablist" aria-label="Bagian kanal">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls={`panel-${t.id}`}
            className={tab === t.id ? 'tab is-active' : 'tab'}
            onClick={() => pickTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === 'video' ? <ChannelVideos username={user.username} isOwn={isOwn} /> : <ChannelAbout user={user} />}
      </div>
    </div>
  )
}

/** Tab Video: video terbaru dibuat besar, sisanya di grid. */
function ChannelVideos({ username, isOwn }: { username: string; isOwn: boolean }) {
  const feed = usePagedVideos(`channel:${username}`, (token, signal) => listChannelVideos(username, token, signal))
  const [latest, ...rest] = feed.videos
  const empty = isOwn ? 'Kamu belum punya video yang siap ditonton. Unggah video pertamamu!' : 'Kanal ini belum punya video.'

  // Belum ada video sama sekali: cukup grid (yang menampilkan memuat, error, atau kosong).
  if (!latest) return <VideoGrid feed={feed} empty={empty} hideOwner />

  return (
    <div className="channel-videos">
      <LatestVideo video={latest} />
      {(rest.length > 0 || feed.hasMore || feed.error) && (
        <section className="channel-section">
          <h2 className="section-title">Video lainnya</h2>
          <VideoGrid feed={{ ...feed, videos: rest }} empty="" hideOwner />
        </section>
      )}
    </div>
  )
}

function LatestVideo({ video }: { video: Video }) {
  return (
    <section className="channel-latest">
      <Link to={`/watch/${video.id}`} className="channel-latest-thumb" aria-label={video.title}>
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        <span className="channel-latest-badge">Video terbaru</span>
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </Link>
      <div className="channel-latest-text">
        <Link to={`/watch/${video.id}`} className="channel-latest-title">
          {video.title}
        </Link>
        <span className="muted small">
          {formatCount(video.view_count)} tayangan · {timeAgo(video.created_at)}
        </span>
        {video.description && <p className="channel-latest-desc">{video.description}</p>}
      </div>
    </section>
  )
}

/** Tab Tentang: bio lengkap dan data kanal. */
function ChannelAbout({ user }: { user: Profile }) {
  return (
    <section className="channel-about">
      <h2 className="section-title">Tentang</h2>
      <p className="description">{user.bio || 'Kanal ini belum menulis deskripsi.'}</p>
      <dl className="channel-facts">
        <div>
          <dt>Pengikut</dt>
          <dd>{formatCount(user.subscriber_count)}</dd>
        </div>
        <div>
          <dt>Bergabung</dt>
          <dd>{formatDate(user.created_at)}</dd>
        </div>
      </dl>
    </section>
  )
}

/** Banner hiasan dari desain (backend belum punya gambar banner kanal). */
function ChannelBanner() {
  return (
    <div className="channel-banner" aria-hidden="true">
      <BannerTv size={150} screen="#4FE3C1" />
      <BannerTv size={110} screen="#FF6B8B" />
      <BannerTv size={190} screen="#8FD8FF" />
    </div>
  )
}

function BannerTv({ size, screen }: { size: number; screen: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64">
      <rect x="9" y="13" width="46" height="31" rx="10" fill={screen} />
      <path d="M15 41 L49 41 L32 62 Z" fill="#F5B451" />
    </svg>
  )
}

/** Baris pertama bio untuk ringkasan di kepala kanal; versi lengkap ada di tab Tentang. */
function firstLine(bio: string): string {
  return bio.split('\n', 1)[0]
}
