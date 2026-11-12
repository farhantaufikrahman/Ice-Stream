import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage, isAbortError } from '../api/client'
import { getChannelStats, listMyVideos } from '../api/endpoints'
import type { ChannelStats, StatsDays, Video } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { StatusChip } from '../components/studio/StatusChip'
import { StudioLayout } from '../components/studio/StudioLayout'
import { StatsKpis } from '../components/studio/StatsKpis'
import { ViewsChart } from '../components/studio/ViewsChart'
import { ThemeToggle } from '../components/ThemeToggle'
import { Thumbnail } from '../components/Thumbnail'
import { formatPeriod } from '../lib/channelStats'
import { formatCount, formatDate } from '../lib/format'
import { videoDate } from '../lib/visibility'

const RANGES: StatsDays[] = [7, 28, 90]
const DEFAULT_DAYS: StatsDays = 28
/** Jumlah baris "Konten terbaru" di dasbor; sisanya di menu Konten. */
const RECENT_COUNT = 5

function parseDays(raw: string | null): StatsDays {
  const n = Number(raw)
  return RANGES.find((d) => d === n) ?? DEFAULT_DAYS
}

interface Loaded<T> {
  key: string
  data?: T
  error?: string
}

/**
 * Creator Studio, menu Dasbor (desain Studio.dc.html): kartu angka, grafik
 * penayangan per hari, dan konten terbaru. Rentang waktu disimpan di URL
 * (?days=7), jadi tombol Back dan link yang dibagikan tetap sama.
 *
 * Kartu "Waktu tonton" di desain belum ditampilkan karena backend belum
 * mencatat lama menonton per hari; tempatnya diisi Komentar diterima.
 */
export function StudioDashboardPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const days = parseDays(params.get('days'))
  const [reloads, setReloads] = useState(0)
  const [stats, setStats] = useState<Loaded<ChannelStats> | null>(null)
  const [recent, setRecent] = useState<Loaded<Video[]> | null>(null)
  const userKey = user?.id ?? ''
  const statsKey = `${userKey}|${days}|${reloads}`
  const recentKey = `${userKey}|${reloads}`

  useEffect(() => {
    if (!userKey) return
    const ctrl = new AbortController()
    getChannelStats(days, ctrl.signal)
      .then((data) => setStats({ key: statsKey, data }))
      .catch((err: unknown) => {
        if (!isAbortError(err)) setStats({ key: statsKey, error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [days, statsKey, userKey])

  useEffect(() => {
    if (!userKey) return
    const ctrl = new AbortController()
    listMyVideos('', ctrl.signal)
      .then((page) => setRecent({ key: recentKey, data: page.videos.slice(0, RECENT_COUNT) }))
      .catch((err: unknown) => {
        if (!isAbortError(err)) setRecent({ key: recentKey, error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [recentKey, userKey])

  // Data lama (rentang sebelumnya) tidak ditampilkan saat rentang baru dimuat.
  const currentStats = stats?.key === statsKey ? stats : null
  const currentRecent = recent?.key === recentKey ? recent : null
  const name = user?.display_name || user?.username || ''
  const retry = () => setReloads((n) => n + 1)

  const pickDays = (d: StatsDays) =>
    setParams(
      (p) => {
        const next = new URLSearchParams(p)
        if (d === DEFAULT_DAYS) next.delete('days')
        else next.set('days', String(d))
        return next
      },
      { replace: true },
    )

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Dasbor channel</h1>
          <span className="muted small">
            {user && `@${user.username}`}
            {currentStats?.data && ` · ${formatPeriod(currentStats.data.daily)}`}
          </span>
        </div>
        <div className="header-actions">
          <div className="range-group" role="group" aria-label="Rentang waktu">
            {RANGES.map((d) => (
              <button key={d} type="button" className="range-btn" aria-pressed={d === days} onClick={() => pickDays(d)}>
                {d} hari
              </button>
            ))}
          </div>
          <ThemeToggle />
          {user && <Avatar name={name} url={user.avatar_url} size={40} />}
        </div>
      </div>

      {currentStats?.error ? (
        <ErrorBox message={currentStats.error} onRetry={retry} />
      ) : !currentStats?.data ? (
        <p className="muted">Memuat statistik…</p>
      ) : (
        <>
          <StatsKpis stats={currentStats.data} />
          <ViewsChart daily={currentStats.data.daily} />
        </>
      )}

      <section className="panel">
        <div className="panel-head">
          <h2 className="panel-subtitle">Konten terbaru</h2>
          <Link to="/studio/content" className="small">
            Kelola semua
          </Link>
        </div>
        {currentRecent?.error ? (
          <ErrorBox message={currentRecent.error} onRetry={retry} />
        ) : !currentRecent?.data ? (
          <p className="muted">Memuat…</p>
        ) : currentRecent.data.length === 0 ? (
          <div className="empty">
            <p>Belum ada video.</p>
            <Link to="/upload" className="btn btn-primary">
              Unggah video pertamamu
            </Link>
          </div>
        ) : (
          <RecentTable videos={currentRecent.data} />
        )}
      </section>
    </StudioLayout>
  )
}

/** Tabel "Konten terbaru" (desain), hanya baca. Edit dan hapus ada di menu Konten. */
function RecentTable({ videos }: { videos: Video[] }) {
  return (
    <div className="table-wrap">
      <table className="studio-table">
        <thead>
          <tr>
            <th scope="col">Video</th>
            <th scope="col">Status</th>
            <th scope="col">Tanggal</th>
            <th scope="col" className="num">
              Tayangan
            </th>
            <th scope="col" className="num">
              Suka
            </th>
            <th scope="col" className="num">
              Komentar
            </th>
          </tr>
        </thead>
        <tbody>
          {videos.map((v) => {
            const ready = v.status === 'READY'
            return (
              <tr key={v.id}>
                <td>
                  <div className="studio-video">
                    <div className="studio-thumb studio-thumb-small">
                      <Thumbnail url={v.thumbnail_url} title={v.title} />
                    </div>
                    {ready ? (
                      <Link to={`/watch/${v.id}`} className="studio-video-title">
                        {v.title}
                      </Link>
                    ) : (
                      <span className="studio-video-title">{v.title}</span>
                    )}
                  </div>
                </td>
                <td>
                  <StatusChip status={v.status} />
                </td>
                <td className="nowrap muted">{formatDate(videoDate(v))}</td>
                <td className="num">{ready ? formatCount(v.view_count) : '—'}</td>
                <td className="num">{ready ? formatCount(v.like_count) : '—'}</td>
                <td className="num">{ready ? formatCount(v.comment_count) : '—'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
