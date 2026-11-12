import { Link } from 'react-router'
import type { TrendingVideo } from '../../api/types'
import { useTrending } from '../../hooks/useTrending'
import { categoryLabel } from '../../lib/categories'
import { ownerName } from '../../lib/discover'
import { formatCount } from '../../lib/format'
import { Thumbnail } from '../Thumbnail'

/** Beranda menampilkan 5 video teratas (desain Home.dc.html). */
const HOME_TRENDING = 5

/**
 * Bagian atas beranda: "Sorotan minggu ini" (Trending peringkat 1) dan
 * daftar Trending 5 teratas.
 *
 * Trending = tayangan 7 hari terakhir terbanyak (langkah 24a). Bagian ini
 * pelengkap: selama memuat, jika gagal, atau jika belum ada tayangan
 * minggu ini, bagian ini tidak ditampilkan sama sekali. Tidak ada data
 * contoh.
 */
export function TrendingSection({ category }: { category: string }) {
  const { videos } = useTrending(category, HOME_TRENDING)
  if (!videos || videos.length === 0) return null
  const [top] = videos
  const allLink = category ? `/trending?category=${encodeURIComponent(category)}` : '/trending'
  const label = categoryLabel(category)

  return (
    <section className="home-hero" aria-label="Trending minggu ini">
      <Spotlight video={top} />
      <aside className="trending-box">
        <div className="trending-head">
          <h2 className="section-title">{label ? `Trending ${label}` : 'Trending'}</h2>
          <Link to={allLink} className="small">
            Lihat semua
          </Link>
        </div>
        <ol className="trending-list">
          {videos.map((v, i) => (
            <li key={v.id} className="trending-item">
              <TrendingRank rank={i + 1} />
              <div className="trending-text">
                <Link to={`/watch/${v.id}`} className="trending-title">
                  {v.title}
                </Link>
                <span className="muted small">
                  {ownerName(v)} · {formatCount(v.recent_views)} tayangan minggu ini
                </span>
              </div>
            </li>
          ))}
        </ol>
      </aside>
    </section>
  )
}

/** Kartu besar: peringkat 1 Trending, dengan thumbnail asli videonya. */
function Spotlight({ video }: { video: TrendingVideo }) {
  return (
    <Link to={`/watch/${video.id}`} className="spotlight">
      <Thumbnail url={video.thumbnail_url} title="" />
      <div className="spotlight-text">
        <span className="spotlight-badge">Sorotan minggu ini</span>
        <h1 className="spotlight-title">{video.title}</h1>
        <p className="spotlight-meta">
          {ownerName(video)} · {formatCount(video.view_count)} tayangan
          {video.danmaku_count > 0 && ` · ${formatCount(video.danmaku_count)} danmaku`}
        </p>
        <span className="spotlight-cta">
          <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
            <polygon points="6 4 20 12 6 20 6 4" fill="currentColor" />
          </svg>
          Tonton sekarang
        </span>
      </div>
    </Link>
  )
}

/** Angka peringkat; 1 sampai 3 berwarna amber seperti desain. */
export function TrendingRank({ rank }: { rank: number }) {
  return (
    <span className={rank <= 3 ? 'trending-rank is-top' : 'trending-rank'} aria-label={`Peringkat ${rank}`}>
      {rank}
    </span>
  )
}
