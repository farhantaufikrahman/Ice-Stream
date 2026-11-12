import { useState } from 'react'
import { listRecommended } from '../../api/endpoints'
import { useAuth } from '../../auth/useAuth'
import { usePagedVideos } from '../../hooks/usePagedVideos'
import { categoryLabel } from '../../lib/categories'
import { newSeed, rememberSeed, sessionSeed } from '../../lib/seed'
import { VideoGrid } from '../VideoGrid'

/** Beranda menampilkan 12 rekomendasi (maksimal dari server 24). */
const HOME_RECOMMENDED = 12

/**
 * "Rekomendasi untukmu" (desain Home.dc.html, langkah 27).
 *
 * Susunannya dibuat VideoService dari sinyal yang jelas: video populer,
 * video baru, kategori yang sering kamu tonton, dan kanal yang kamu ikuti,
 * ditambah sedikit acak. "Muat ulang" mengganti bagian acaknya (seed).
 * Video yang sudah kamu tonton sampai habis dan video milikmu sendiri
 * tidak direkomendasikan.
 *
 * Bagian ini disembunyikan jika tidak ada video sama sekali; "Video
 * terbaru" di bawahnya yang menampilkan pesan kosong.
 */
export function RecommendedSection({ category }: { category: string }) {
  const { status } = useAuth()
  const [seed, setSeed] = useState(sessionSeed)
  // Daftar dimuat ulang jika seed, kategori, atau status login berubah.
  const feed = usePagedVideos(`recommend:${status}:${category}:${seed}`, async (_token, signal) => ({
    videos: await listRecommended(seed, category, HOME_RECOMMENDED, signal),
    next_page_token: '',
  }))
  const label = categoryLabel(category)

  if (!feed.loading && !feed.error && feed.videos.length === 0) return null
  return (
    <section className="recommend-section" aria-labelledby="home-recommend-title">
      <div className="section-head">
        <h2 id="home-recommend-title" className="section-title">
          {label ? `Rekomendasi ${label} untukmu` : 'Rekomendasi untukmu'}
        </h2>
        <button type="button" className="btn btn-small" disabled={feed.loading} onClick={() => setSeed(rememberSeed(newSeed()))}>
          Muat ulang
        </button>
      </div>
      <VideoGrid feed={feed} empty="" />
    </section>
  )
}
