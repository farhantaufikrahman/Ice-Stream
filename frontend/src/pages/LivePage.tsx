import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage } from '../api/client'
import { listLive } from '../api/endpoints'
import type { LiveStream } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'
import { LiveGrid } from '../components/live/LiveCard'
import { CATEGORIES, categoryLabel, isCategory } from '../lib/categories'

/** Batas server untuk satu kali GET /live. */
const MAX_LIVE = 50

interface Loaded {
  key: string
  streams?: LiveStream[]
  error?: string
}

/**
 * Halaman Live (/live, menu "Live" di header desain): semua siaran yang
 * sedang berlangsung, bisa dipilih per kategori (?category=game).
 */
export function LivePage() {
  const { status } = useAuth()
  const [params, setParams] = useSearchParams()
  const raw = params.get('category')
  const category = isCategory(raw) ? raw : ''
  const [reloads, setReloads] = useState(0)
  const key = `${category}|${reloads}`
  const [loaded, setLoaded] = useState<Loaded | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    listLive({ limit: MAX_LIVE, category }, ctrl.signal)
      .then((streams) => setLoaded({ key, streams }))
      .catch((err) => {
        if (!ctrl.signal.aborted) setLoaded({ key, error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [key, category])

  const current = loaded?.key === key ? loaded : null
  const label = categoryLabel(category)
  const chip = (text: string, value: string) => (
    <button
      key={text}
      type="button"
      role="tab"
      aria-selected={category === value}
      className={category === value ? 'chip-tab is-active' : 'chip-tab'}
      onClick={() => setParams(value ? { category: value } : {})}
    >
      {text}
    </button>
  )

  return (
    <>
      <div className="list-page-head">
        <h1 className="page-title live-title">
          <span className="live-dot" aria-hidden="true" />
          {label ? `Live ${label}` : 'Sedang live'}
        </h1>
        {status === 'authenticated' && (
          <Link to="/studio/live" className="btn btn-live">
            <span className="live-dot is-dark" aria-hidden="true" />
            Mulai live
          </Link>
        )}
      </div>
      <div className="chips chips-scroll" role="tablist" aria-label="Pilih kategori siaran">
        {chip('Semua', '')}
        {CATEGORIES.map((c) => chip(c.label, c.value))}
      </div>
      {!current && <p className="muted center">Memuat…</p>}
      {current?.error && <ErrorBox message={current.error} onRetry={() => setReloads((n) => n + 1)} />}
      {current?.streams?.length === 0 && (
        <p className="muted center">{label ? `Belum ada yang live di kategori ${label}.` : 'Belum ada yang sedang live.'}</p>
      )}
      {current?.streams && current.streams.length > 0 && <LiveGrid streams={current.streams} />}
    </>
  )
}
