import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { listLive } from '../../api/endpoints'
import type { LiveStream } from '../../api/types'
import { LiveGrid } from '../live/LiveCard'

/** Beranda menampilkan 4 siaran (desain Home.dc.html). */
const HOME_LIVE = 4

/**
 * "Sedang live" di beranda (langkah 25c), mengikuti chip kategori yang
 * dipilih. Seperti Trending, bagian ini pelengkap: selama memuat, jika
 * gagal, atau jika tidak ada yang sedang live, tidak ditampilkan.
 */
export function LiveSection({ category }: { category: string }) {
  const [streams, setStreams] = useState<LiveStream[] | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    listLive({ limit: HOME_LIVE, category }, ctrl.signal)
      .then(setStreams)
      .catch(() => {
        if (!ctrl.signal.aborted) setStreams([])
      })
    return () => ctrl.abort()
  }, [category])

  if (!streams || streams.length === 0) return null
  return (
    <section className="live-section" aria-labelledby="home-live-title">
      <div className="live-section-head">
        <h2 id="home-live-title" className="section-title live-title">
          <span className="live-dot" aria-hidden="true" />
          Sedang live
        </h2>
        <Link to={category ? `/live?category=${encodeURIComponent(category)}` : '/live'} className="small">
          Lihat semua
        </Link>
      </div>
      <LiveGrid streams={streams} />
    </section>
  )
}
