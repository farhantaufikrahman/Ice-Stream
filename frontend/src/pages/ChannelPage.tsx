import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { listChannelPlaylists, listChannelVideos } from '../api/endpoints'
import type { ChannelCounts, Playlist, Profile, Video } from '../api/types'
import { Avatar } from '../components/Avatar'
import { SubscribeButton } from '../components/channel/SubscribeButton'
import { ErrorBox } from '../components/ErrorBox'
import { PlaylistCard } from '../components/playlist/PlaylistCard'
import { PlaylistFormDialog } from '../components/playlist/PlaylistFormDialog'
import { Thumbnail } from '../components/Thumbnail'
import { VideoGrid } from '../components/VideoGrid'
import { useChannel } from '../hooks/useChannel'
import { usePagedList } from '../hooks/usePagedList'
import { usePagedVideos } from '../hooks/usePagedVideos'
import { CHANNEL_SORTS, channelSortFromParam, withParam, type ChannelSort } from '../lib/discover'
import { formatCount, formatDate, formatDuration, timeAgo } from '../lib/format'
import { videoDate } from '../lib/visibility'

type Tab = 'video' | 'playlist' | 'tentang'

const TABS: { id: Tab; label: string }[] = [
  { id: 'video', label: 'Video' },
  { id: 'playlist', label: 'Playlist' },
  { id: 'tentang', label: 'Tentang' },
]

function tabFromParam(value: string | null): Tab {
  return TABS.find((t) => t.id === value)?.id ?? 'video'
}

/**
 * Halaman kanal /channel/:username (desain Channel.dc.html).
 *
 * Angka video, total tayangan, dan suka serta "Video unggulan" ada sejak
 * langkah 27 (kreator memilih video unggulan di Studio > Pengaturan; jika
 * belum memilih, video terbaru yang ditampilkan besar). Bagian desain yang
 * belum punya data di backend sengaja tidak ditampilkan: tombol Kirim
 * pesan dan tab Live. Urutan Terbaru/Terpopuler di tab Video ada sejak
 * langkah 24a (?sort=popular di URL).
 */
export function ChannelPage() {
  const { username = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const tab = tabFromParam(params.get('tab'))
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
  const sort = channelSortFromParam(params.get('sort'))
  const pickTab = (t: Tab) => setParams(t === 'video' ? {} : { tab: t }, { replace: true })
  const pickSort = (v: ChannelSort) => setParams((p) => withParam(p, 'sort', v, 'newest'), { replace: true })

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
            {channel.stats && (
              <>
                <span>
                  <strong>{formatCount(channel.stats.video_count)}</strong> <span className="muted">video</span>
                </span>
                <span>
                  <strong>{formatCount(channel.stats.total_views)}</strong> <span className="muted">total tayangan</span>
                </span>
                <span>
                  <strong>{formatCount(channel.stats.total_likes)}</strong> <span className="muted">suka</span>
                </span>
              </>
            )}
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
        {tab === 'video' && (
          <ChannelVideos
            username={user.username}
            isOwn={isOwn}
            sort={sort}
            onSort={pickSort}
            featured={channel.featured_video ?? null}
          />
        )}
        {tab === 'playlist' && <ChannelPlaylists key={`${user.username}|${isOwn}`} username={user.username} isOwn={isOwn} />}
        {tab === 'tentang' && <ChannelAbout user={user} stats={channel.stats} />}
      </div>
    </div>
  )
}

interface ChannelVideosProps {
  username: string
  isOwn: boolean
  sort: ChannelSort
  onSort: (sort: ChannelSort) => void
  /** Video unggulan pilihan kreator, null jika belum dipilih. */
  featured: Video | null
}

/**
 * Tab Video. Urutan Terbaru: Video unggulan (atau video terbaru jika
 * kreator belum memilih) dibuat besar, sisanya di grid. Urutan
 * Terpopuler: semua di grid, tayangan terbanyak dulu.
 */
function ChannelVideos({ username, isOwn, sort, onSort, featured }: ChannelVideosProps) {
  // sort ikut di key: ganti urutan = mulai lagi dari halaman pertama.
  const feed = usePagedVideos(`channel:${username}|${sort}`, (token, signal) => listChannelVideos(username, token, signal, sort))
  const empty = isOwn ? 'Kamu belum punya video yang siap ditonton. Unggah video pertamamu!' : 'Kanal ini belum punya video.'
  const [latest, ...others] = feed.videos
  // Video unggulan tidak diulang di grid di bawahnya.
  const big = featured ?? latest
  const rest = featured ? feed.videos.filter((v) => v.id !== featured.id) : others

  // Belum ada video sama sekali: cukup grid (yang menampilkan memuat, error,
  // atau kosong), tanpa tombol urutan yang tidak berguna.
  if (feed.videos.length === 0 && !featured) return <VideoGrid feed={feed} empty={empty} hideOwner />

  const sortButtons = (
    <div className="sort-toggle" role="group" aria-label="Urutan video">
      {CHANNEL_SORTS.map((s) => (
        <button
          key={s.value}
          type="button"
          aria-pressed={sort === s.value}
          className={sort === s.value ? 'filter-btn is-active' : 'filter-btn'}
          onClick={() => onSort(s.value)}
        >
          {s.label}
        </button>
      ))}
    </div>
  )

  if (sort === 'popular') {
    return (
      <section className="channel-section">
        <div className="section-head">
          <h2 className="section-title">Video terpopuler</h2>
          {sortButtons}
        </div>
        <VideoGrid feed={feed} empty="" hideOwner />
      </section>
    )
  }

  return (
    <div className="channel-videos">
      <BigVideo video={big} badge={featured ? 'Video unggulan' : 'Video terbaru'} />
      <section className="channel-section">
        <div className="section-head">
          <h2 className="section-title">Video lainnya</h2>
          {sortButtons}
        </div>
        {rest.length === 0 && !feed.hasMore && !feed.error ? (
          <p className="muted">Belum ada video lain.</p>
        ) : (
          <VideoGrid feed={{ ...feed, videos: rest }} empty="" hideOwner />
        )}
      </section>
    </div>
  )
}

/** Video besar di atas tab Video: Video unggulan, atau video terbaru. */
function BigVideo({ video, badge }: { video: Video; badge: string }) {
  return (
    <section className="channel-latest" aria-label={badge}>
      <Link to={`/watch/${video.id}`} className="channel-latest-thumb" aria-label={video.title}>
        <Thumbnail url={video.thumbnail_url} title={video.title} />
        <span className="channel-latest-badge">{badge}</span>
        {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
      </Link>
      <div className="channel-latest-text">
        <Link to={`/watch/${video.id}`} className="channel-latest-title">
          {video.title}
        </Link>
        <span className="muted small">
          {formatCount(video.view_count)} tayangan · {timeAgo(videoDate(video))}
        </span>
        {video.description && <p className="channel-latest-desc">{video.description}</p>}
      </div>
    </section>
  )
}

/**
 * Tab Playlist: playlist kanal, terbaru dulu. Di kanal sendiri playlist
 * PRIVATE ikut tampil (bertanda gembok) dan ada tombol Playlist baru.
 */
function ChannelPlaylists({ username, isOwn }: { username: string; isOwn: boolean }) {
  const list = usePagedList<Playlist>((token, signal) =>
    listChannelPlaylists(username, token, signal).then((p) => ({ items: p.playlists, next: p.next_page_token })),
  )
  const [creating, setCreating] = useState(false)

  return (
    <section className="channel-playlists">
      {isOwn && (
        <div className="list-page-head">
          <h2 className="section-title">Playlist</h2>
          <button type="button" className="btn btn-primary" onClick={() => setCreating(true)}>
            Playlist baru
          </button>
        </div>
      )}
      {list.items.length > 0 && (
        <div className="video-grid">
          {list.items.map((p) => (
            <PlaylistCard key={p.id} playlist={p} />
          ))}
        </div>
      )}
      {!list.loading && !list.error && list.items.length === 0 && (
        <div className="empty">
          <p>{isOwn ? 'Kamu belum punya playlist.' : 'Kanal ini belum punya playlist publik.'}</p>
          {isOwn && <p className="muted small">Buat playlist di sini, atau tekan Simpan di bawah video.</p>}
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
      {creating && (
        <PlaylistFormDialog
          onClose={() => setCreating(false)}
          onSaved={(p) => {
            list.update((items) => [p, ...items])
            setCreating(false)
          }}
        />
      )}
    </section>
  )
}

/** Tab Tentang: bio lengkap dan data kanal. */
function ChannelAbout({ user, stats }: { user: Profile; stats?: ChannelCounts }) {
  return (
    <section className="channel-about">
      <h2 className="section-title">Tentang</h2>
      <p className="description">{user.bio || 'Kanal ini belum menulis deskripsi.'}</p>
      <dl className="channel-facts">
        <div>
          <dt>Pengikut</dt>
          <dd>{formatCount(user.subscriber_count)}</dd>
        </div>
        {stats && (
          <>
            <div>
              <dt>Video</dt>
              <dd>{formatCount(stats.video_count)}</dd>
            </div>
            <div>
              <dt>Total tayangan</dt>
              <dd>{formatCount(stats.total_views)}</dd>
            </div>
          </>
        )}
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
