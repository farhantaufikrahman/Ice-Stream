import { Link, useSearchParams } from 'react-router'
import { ErrorBox } from '../components/ErrorBox'
import { TrendingRank } from '../components/home/TrendingSection'
import { Thumbnail } from '../components/Thumbnail'
import { useTrending } from '../hooks/useTrending'
import { CATEGORIES, categoryLabel, isCategory } from '../lib/categories'
import { ownerName } from '../lib/discover'
import { formatCount, formatDuration } from '../lib/format'

/** Batas backend (maxTrendingLimit di internal/video/discover.go). */
const PAGE_LIMIT = 50

/**
 * Halaman /trending ("Lihat semua" dari beranda): 50 video dengan
 * tayangan 7 hari terakhir terbanyak, bisa dipilih per kategori
 * (?category=game).
 */
export function TrendingPage() {
  const [params, setParams] = useSearchParams()
  const raw = params.get('category')
  const category = isCategory(raw) ? raw : ''
  const { videos, error, retry } = useTrending(category, PAGE_LIMIT)
  const label = categoryLabel(category)

  const chip = (text: string, value: string) => (
    <button
      key={text}
      type="button"
      role="tab"
      aria-selected={category === value}
      className={category === value ? 'chip-tab is-active' : 'chip-tab'}
      onClick={() => setParams(value ? { category: value } : {}, { replace: true })}
    >
      {text}
    </button>
  )

  return (
    <div className="trending-page">
      <div>
        <h1 className="page-title">{label ? `Trending ${label}` : 'Trending'}</h1>
        <p className="muted">Video dengan tayangan terbanyak dalam 7 hari terakhir.</p>
      </div>
      <div className="chips chips-scroll" role="tablist" aria-label="Pilih kategori">
        {chip('Semua', '')}
        {CATEGORIES.map((c) => chip(c.label, c.value))}
      </div>
      {error && <ErrorBox message={error} onRetry={retry} />}
      {!videos && !error && <p className="muted center">Memuat…</p>}
      {videos && videos.length === 0 && !error && (
        <p className="muted center">{label ? `Belum ada video ${label} yang ditonton minggu ini.` : 'Belum ada video yang ditonton minggu ini.'}</p>
      )}
      {videos && videos.length > 0 && (
        <ol className="trending-rows">
          {videos.map((v, i) => (
            <li key={v.id}>
              <Link to={`/watch/${v.id}`} className="trending-row">
                <TrendingRank rank={i + 1} />
                <div className="trending-row-thumb">
                  <Thumbnail url={v.thumbnail_url} title={v.title} />
                  {v.duration_ms ? <span className="duration">{formatDuration(v.duration_ms)}</span> : null}
                </div>
                <div className="trending-text">
                  <span className="trending-title">{v.title}</span>
                  <span className="muted small">{ownerName(v)}</span>
                  <span className="small">
                    <strong>{formatCount(v.recent_views)}</strong> <span className="muted">tayangan minggu ini · {formatCount(v.view_count)} total</span>
                  </span>
                </div>
              </Link>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}
