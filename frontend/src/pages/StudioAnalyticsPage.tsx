import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage, isAbortError } from '../api/client'
import { getChannelStats } from '../api/endpoints'
import type { ChannelStats, StatsDays, TopVideo } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'
import { StatsKpis } from '../components/studio/StatsKpis'
import { StudioLayout } from '../components/studio/StudioLayout'
import { VideoFilter } from '../components/studio/VideoFilter'
import { ViewsChart } from '../components/studio/ViewsChart'
import { ThemeToggle } from '../components/ThemeToggle'
import { Thumbnail } from '../components/Thumbnail'
import { useMyVideoOptions } from '../hooks/useMyVideoOptions'
import { formatNumber, formatPeriod } from '../lib/channelStats'

const RANGES: StatsDays[] = [7, 28, 90]
const DEFAULT_DAYS: StatsDays = 28

function parseDays(raw: string | null): StatsDays {
  const n = Number(raw)
  return RANGES.find((d) => d === n) ?? DEFAULT_DAYS
}

interface Loaded {
  key: string
  data?: ChannelStats
  error?: string
}

/**
 * Creator Studio, menu Analitik (langkah 26): angka dan grafik yang sama
 * dengan Dasbor, bisa dipersempit ke satu video, plus tabel video dengan
 * tayangan terbanyak di periode itu. Pilihan disimpan di URL
 * (?days=7&video=42).
 */
export function StudioAnalyticsPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const days = parseDays(params.get('days'))
  const videoId = params.get('video') ?? ''
  const [reloads, setReloads] = useState(0)
  const [stats, setStats] = useState<Loaded | null>(null)
  const userKey = user?.id ?? ''
  const key = `${userKey}|${days}|${videoId}|${reloads}`
  const options = useMyVideoOptions(userKey)

  useEffect(() => {
    if (!userKey) return
    const ctrl = new AbortController()
    getChannelStats(days, ctrl.signal, videoId || undefined)
      .then((data) => setStats({ key, data }))
      .catch((err: unknown) => {
        if (!isAbortError(err)) setStats({ key, error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [days, videoId, key, userKey])

  const current = stats?.key === key ? stats : null
  const setParam = (name: string, value: string, fallback: string) =>
    setParams(
      (p) => {
        const next = new URLSearchParams(p)
        if (value === fallback) next.delete(name)
        else next.set(name, value)
        return next
      },
      { replace: true },
    )

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Analitik</h1>
          <span className="muted small">
            {user && `@${user.username}`}
            {current?.data && ` · ${formatPeriod(current.data.daily)}`}
          </span>
        </div>
        <div className="header-actions">
          <div className="range-group" role="group" aria-label="Rentang waktu">
            {RANGES.map((d) => (
              <button
                key={d}
                type="button"
                className="range-btn"
                aria-pressed={d === days}
                onClick={() => setParam('days', String(d), String(DEFAULT_DAYS))}
              >
                {d} hari
              </button>
            ))}
          </div>
          <ThemeToggle />
        </div>
      </div>

      <VideoFilter options={options} value={videoId} onChange={(id) => setParam('video', id, '')} />

      {current?.error ? (
        <ErrorBox message={current.error} onRetry={() => setReloads((n) => n + 1)} />
      ) : !current?.data ? (
        <p className="muted">Memuat statistik…</p>
      ) : (
        <>
          <StatsKpis stats={current.data} />
          <ViewsChart daily={current.data.daily} />
          {!videoId && <TopVideos videos={current.data.top_videos} days={days} />}
        </>
      )}
    </StudioLayout>
  )
}

function TopVideos({ videos, days }: { videos: TopVideo[]; days: StatsDays }) {
  return (
    <section className="panel">
      <div className="panel-head">
        <h2 className="panel-subtitle">Video terpopuler</h2>
        <span className="small muted">Tayangan {days} hari terakhir</span>
      </div>
      {videos.length === 0 ? (
        <p className="muted">Belum ada tayangan di periode ini.</p>
      ) : (
        <div className="table-wrap">
          <table className="studio-table">
            <thead>
              <tr>
                <th scope="col">Video</th>
                <th scope="col" className="num">
                  Tayangan
                </th>
                <th scope="col" className="num">
                  <span className="sr-only">Aksi</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {videos.map((v) => (
                <tr key={v.video_id}>
                  <td>
                    <div className="studio-video">
                      <div className="studio-thumb studio-thumb-small">
                        <Thumbnail url={v.thumbnail_url} title={v.title} />
                      </div>
                      <Link to={`/watch/${v.video_id}`} className="studio-video-title">
                        {v.title}
                      </Link>
                    </div>
                  </td>
                  <td className="num">{formatNumber(v.views)}</td>
                  <td className="num">
                    <Link to={`/studio/analytics?days=${days}&video=${v.video_id}`} className="btn btn-small">
                      Detail
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
