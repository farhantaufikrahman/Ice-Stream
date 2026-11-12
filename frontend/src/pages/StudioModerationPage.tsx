import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage, isAbortError } from '../api/client'
import { dismissReports, listBlockedVideos, listReportedVideos, listVideoReports, unblockVideo } from '../api/endpoints'
import type { BlockedVideo, ReportedVideo, Video, VideoReport } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'
import { BlockVideoDialog } from '../components/moderation/BlockVideoDialog'
import { StudioLayout } from '../components/studio/StudioLayout'
import { ThemeToggle } from '../components/ThemeToggle'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedList, type PagedList } from '../hooks/usePagedList'
import { formatDuration, timeAgo } from '../lib/format'
import { reportReasonLabel } from '../lib/report'
import { visibilityLabel } from '../lib/visibility'

type Tab = 'reports' | 'blocked'

const TABS: { id: Tab; label: string }[] = [
  { id: 'reports', label: 'Laporan terbuka' },
  { id: 'blocked', label: 'Diturunkan' },
]

/**
 * Creator Studio, menu Moderasi (langkah 28). Hanya untuk admin: menu ini
 * tidak tampil untuk user biasa, dan server tetap menolak (403) jika
 * halaman ini dibuka langsung lewat alamatnya.
 */
export function StudioModerationPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab: Tab = params.get('tab') === 'blocked' ? 'blocked' : 'reports'
  const isAdmin = user?.role === 'ADMIN'

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Moderasi</h1>
          <span className="muted small">Tinjau laporan penonton, turunkan video yang melanggar, atau pulihkan video.</span>
        </div>
        <div className="header-actions">
          <ThemeToggle />
        </div>
      </div>
      {!isAdmin ? (
        <section className="panel">
          <p>Halaman ini hanya untuk admin.</p>
          <p className="muted small">Jika akunmu baru dijadikan admin, keluar lalu masuk lagi supaya izinnya terbaca.</p>
        </section>
      ) : (
        <>
          <div className="tabs" role="tablist" aria-label="Bagian moderasi">
            {TABS.map((t) => (
              <button
                key={t.id}
                type="button"
                role="tab"
                id={`mod-tab-${t.id}`}
                aria-selected={tab === t.id}
                aria-controls="mod-panel"
                className={tab === t.id ? 'tab is-active' : 'tab'}
                onClick={() => setParams(t.id === 'reports' ? {} : { tab: t.id }, { replace: true })}
              >
                {t.label}
              </button>
            ))}
          </div>
          <div id="mod-panel" role="tabpanel" aria-labelledby={`mod-tab-${tab}`}>
            {tab === 'reports' ? <ReportQueue /> : <BlockedList />}
          </div>
        </>
      )}
    </StudioLayout>
  )
}

/**
 * Halaman berikutnya memakai offset. Setelah ada baris yang dihapus dari
 * layar (diturunkan, ditolak, dipulihkan), offset itu sudah bergeser, jadi
 * tombolnya diganti "Muat ulang" supaya tidak ada baris yang terlewat.
 */
function useResettable<T>(fetchPage: (token: string, signal: AbortSignal) => Promise<{ items: T[]; next: string }>) {
  const list = usePagedList<T>(fetchPage)
  const [removed, setRemoved] = useState(0)
  const { update } = list
  const remove = useCallback(
    (match: (item: T) => boolean) => {
      update((items) => items.filter((x) => !match(x)))
      setRemoved((n) => n + 1)
    },
    [update],
  )
  return { list, removed, remove }
}

function ListFooter<T>({ list, removed, onReload }: { list: PagedList<T>; removed: number; onReload: () => void }) {
  if (list.error && list.items.length > 0) return <ErrorBox message={list.error} onRetry={list.retry} />
  if (!list.hasMore || list.error) return null
  if (removed > 0) {
    return (
      <button type="button" className="btn load-more" onClick={onReload}>
        Muat ulang daftar
      </button>
    )
  }
  return (
    <button type="button" className="btn load-more" onClick={list.loadMore} disabled={list.loading}>
      {list.loading ? 'Memuat…' : 'Muat lebih banyak'}
    </button>
  )
}

// ---------- Tab "Laporan terbuka" ----------

function ReportQueue() {
  const [generation, setGeneration] = useState(0)
  return <ReportQueueList key={generation} onReload={() => setGeneration((g) => g + 1)} />
}

function ReportQueueList({ onReload }: { onReload: () => void }) {
  const fetchPage = useCallback(async (token: string, signal: AbortSignal) => {
    const page = await listReportedVideos(token, signal)
    return { items: page.videos, next: page.next_page_token }
  }, [])
  const { list, removed, remove } = useResettable<ReportedVideo>(fetchPage)
  const [blocking, setBlocking] = useState<ReportedVideo | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)

  const dismiss = async (item: ReportedVideo) => {
    if (!window.confirm(`Tolak ${item.open_reports} laporan untuk "${item.video.title}"? Video tetap tayang.`)) return
    setBusy(item.video.id)
    setError(null)
    setDone(null)
    try {
      const r = await dismissReports(item.video.id)
      remove((x) => x.video.id === item.video.id)
      setDone(`${r.closed_reports} laporan ditolak. "${item.video.title}" tetap tayang.`)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="panel">
      {done && (
        <p className="moderation-done" role="status">
          {done}
        </p>
      )}
      {error && <ErrorBox message={error} />}
      {list.error && list.items.length === 0 ? (
        <ErrorBox message={list.error} onRetry={list.retry} />
      ) : list.loading && list.items.length === 0 ? (
        <p className="muted">Memuat…</p>
      ) : list.items.length === 0 ? (
        <p className="muted">Tidak ada laporan yang perlu ditinjau.</p>
      ) : (
        <ul className="studio-list">
          {list.items.map((item) => (
            <ReportRow
              key={item.video.id}
              item={item}
              busy={busy !== null}
              dismissing={busy === item.video.id}
              onDismiss={() => void dismiss(item)}
              onBlock={() => setBlocking(item)}
            />
          ))}
        </ul>
      )}
      <ListFooter list={list} removed={removed} onReload={onReload} />
      {blocking && (
        <BlockVideoDialog
          videoId={blocking.video.id}
          title={blocking.video.title}
          suggested={blocking.reasons[0]?.reason}
          onClose={() => setBlocking(null)}
          onBlocked={(r) => {
            remove((x) => x.video.id === blocking.video.id)
            setDone(`"${blocking.video.title}" diturunkan. ${r.closed_reports} laporan ditutup.`)
            setError(null)
            setBlocking(null)
          }}
        />
      )}
    </section>
  )
}

interface ReportRowProps {
  item: ReportedVideo
  busy: boolean
  dismissing: boolean
  onDismiss: () => void
  onBlock: () => void
}

function ReportRow({ item, busy, dismissing, onDismiss, onBlock }: ReportRowProps) {
  const [open, setOpen] = useState(false)
  const v = item.video
  return (
    <li className="studio-item moderation-item">
      <VideoThumb video={v} />
      <div className="studio-item-body">
        <Link to={`/watch/${v.id}`} className="studio-video-title">
          {v.title}
        </Link>
        <OwnerLine video={v} />
        <div className="studio-item-meta">
          <strong className="pink">{item.open_reports} laporan</strong>
          <span>Pertama {timeAgo(item.first_reported_at)}</span>
          <span>Terakhir {timeAgo(item.last_reported_at)}</span>
          {v.visibility !== 'PUBLIC' && <span className="chip">{visibilityLabel(v.visibility)}</span>}
        </div>
        <div className="chip-row">
          {item.reasons.map((r) => (
            <span key={r.reason} className="chip chip-pink">
              {reportReasonLabel(r.reason)} · {r.count}
            </span>
          ))}
        </div>
        <button type="button" className="link-btn small" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          {open ? 'Sembunyikan laporan' : 'Lihat laporan'}
        </button>
        {open && <ReportDetails videoId={v.id} />}
      </div>
      <div className="moderation-actions">
        <button type="button" className="btn btn-small" onClick={onDismiss} disabled={busy} aria-label={`Tolak laporan untuk ${v.title}`}>
          {dismissing ? 'Menolak…' : 'Tolak laporan'}
        </button>
        <button type="button" className="btn btn-small btn-danger" onClick={onBlock} disabled={busy} aria-label={`Turunkan ${v.title}`}>
          Turunkan
        </button>
      </div>
    </li>
  )
}

/** Daftar laporan satu video, dimuat saat "Lihat laporan" ditekan. */
function ReportDetails({ videoId }: { videoId: string }) {
  const [reports, setReports] = useState<VideoReport[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const ctrl = new AbortController()
    listVideoReports(videoId, ctrl.signal)
      .then((r) => {
        setReports(r)
        setError(null)
      })
      .catch((err: unknown) => {
        if (!isAbortError(err)) setError(errorMessage(err))
      })
    return () => ctrl.abort()
  }, [videoId, attempt])

  if (error) return <ErrorBox message={error} onRetry={() => setAttempt((a) => a + 1)} />
  if (!reports) return <p className="muted small">Memuat laporan…</p>
  if (reports.length === 0) return <p className="muted small">Laporannya sudah ditangani.</p>
  return (
    <ul className="report-details">
      {reports.map((r) => (
        <li key={r.reporter_id}>
          <div className="studio-item-meta">
            <strong>{r.reporter ? `@${r.reporter.username}` : `Pengguna #${r.reporter_id}`}</strong>
            <span>{reportReasonLabel(r.reason)}</span>
            <span>{timeAgo(r.reported_at)}</span>
          </div>
          {r.details ? <p className="studio-item-text">{r.details}</p> : <p className="muted small">Tanpa keterangan.</p>}
        </li>
      ))}
    </ul>
  )
}

// ---------- Tab "Diturunkan" ----------

function BlockedList() {
  const [generation, setGeneration] = useState(0)
  return <BlockedListInner key={generation} onReload={() => setGeneration((g) => g + 1)} />
}

function BlockedListInner({ onReload }: { onReload: () => void }) {
  const fetchPage = useCallback(async (token: string, signal: AbortSignal) => {
    const page = await listBlockedVideos(token, signal)
    return { items: page.videos, next: page.next_page_token }
  }, [])
  const { list, removed, remove } = useResettable<BlockedVideo>(fetchPage)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)

  const restore = async (b: BlockedVideo) => {
    if (!window.confirm(`Pulihkan "${b.video.title}"? Visibilitasnya kembali seperti sebelum diturunkan.`)) return
    setBusy(b.video.id)
    setError(null)
    setDone(null)
    try {
      const v = await unblockVideo(b.video.id)
      remove((x) => x.video.id === b.video.id)
      setDone(`"${v.title}" dipulihkan sebagai ${visibilityLabel(v.visibility)}.`)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="panel">
      {done && (
        <p className="moderation-done" role="status">
          {done}
        </p>
      )}
      {error && <ErrorBox message={error} />}
      {list.error && list.items.length === 0 ? (
        <ErrorBox message={list.error} onRetry={list.retry} />
      ) : list.loading && list.items.length === 0 ? (
        <p className="muted">Memuat…</p>
      ) : list.items.length === 0 ? (
        <p className="muted">Belum ada video yang diturunkan.</p>
      ) : (
        <ul className="studio-list">
          {list.items.map((b) => {
            const v = b.video
            const admin = b.moderator ? `@${b.moderator.username}` : 'admin'
            return (
              <li key={v.id} className="studio-item moderation-item">
                <VideoThumb video={v} />
                <div className="studio-item-body">
                  {/* Tanpa tautan: video BLOCKED hanya bisa dibuka pemiliknya. */}
                  <span className="studio-video-title">{v.title}</span>
                  <OwnerLine video={v} />
                  <div className="studio-item-meta">
                    <span className="chip chip-pink">{reportReasonLabel(b.reason)}</span>
                    <span>
                      Diturunkan {timeAgo(b.blocked_at)} oleh {admin}
                    </span>
                  </div>
                  {b.note && <p className="studio-item-text">{b.note}</p>}
                </div>
                <div className="moderation-actions">
                  <button
                    type="button"
                    className="btn btn-small"
                    onClick={() => void restore(b)}
                    disabled={busy !== null}
                    aria-label={`Pulihkan ${v.title}`}
                  >
                    {busy === v.id ? 'Memulihkan…' : 'Pulihkan'}
                  </button>
                </div>
              </li>
            )
          })}
        </ul>
      )}
      <ListFooter list={list} removed={removed} onReload={onReload} />
    </section>
  )
}

// ---------- Bagian kecil ----------

function VideoThumb({ video }: { video: Video }) {
  return (
    <div className="studio-thumb">
      <Thumbnail url={video.thumbnail_url} title={video.title} />
      {video.duration_ms ? <span className="duration">{formatDuration(video.duration_ms)}</span> : null}
    </div>
  )
}

function OwnerLine({ video }: { video: Video }) {
  const o = video.owner
  if (!o) return <span className="muted small">Pemilik #{video.owner_id}</span>
  return (
    <span className="muted small">
      <Link to={`/channel/${o.username}`}>{o.display_name || o.username}</Link> · @{o.username}
    </span>
  )
}
