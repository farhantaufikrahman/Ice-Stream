import { useCallback, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage } from '../api/client'
import { deleteDanmaku, listMyDanmaku } from '../api/endpoints'
import type { ChannelDanmaku, DanmakuMode } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { StudioLayout } from '../components/studio/StudioLayout'
import { VideoFilter } from '../components/studio/VideoFilter'
import { ThemeToggle } from '../components/ThemeToggle'
import { useMyVideoOptions } from '../hooks/useMyVideoOptions'
import { usePagedList } from '../hooks/usePagedList'
import { formatDuration, timeAgo } from '../lib/format'

/** Nama mode danmaku dalam bahasa Indonesia. */
const MODE_LABELS: Record<DanmakuMode, string> = {
  SCROLL: 'Berjalan',
  TOP: 'Atas',
  BOTTOM: 'Bawah',
}

/**
 * Creator Studio, menu Danmaku (langkah 26): danmaku di semua video milik
 * user, terbaru lebih dulu, dengan tombol Hapus (pemilik video boleh
 * menghapus danmaku siapa pun di videonya).
 */
export function StudioDanmakuPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const videoId = params.get('video') ?? ''
  const options = useMyVideoOptions(user?.id ?? '')

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Danmaku</h1>
          {user && <span className="muted small">@{user.username}</span>}
        </div>
        <div className="header-actions">
          <ThemeToggle />
        </div>
      </div>
      <VideoFilter
        options={options}
        value={videoId}
        onChange={(id) => setParams(id ? { video: id } : {}, { replace: true })}
      />
      {/* key: pindah video = daftar dimuat ulang dari awal. */}
      <DanmakuList key={videoId} videoId={videoId} />
    </StudioLayout>
  )
}

function DanmakuList({ videoId }: { videoId: string }) {
  const fetchPage = useCallback(
    async (token: string, signal: AbortSignal) => {
      const page = await listMyDanmaku(videoId, token, signal)
      return { items: page.danmaku, next: page.next_page_token }
    },
    [videoId],
  )
  const list = usePagedList<ChannelDanmaku>(fetchPage)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const remove = async (d: ChannelDanmaku) => {
    if (!window.confirm('Hapus danmaku ini?')) return
    setBusy(d.id)
    setError(null)
    try {
      await deleteDanmaku(d.id)
      list.update((items) => items.filter((x) => x.id !== d.id))
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="panel">
      {error && <ErrorBox message={error} />}
      {list.error && list.items.length === 0 ? (
        <ErrorBox message={list.error} onRetry={list.retry} />
      ) : list.loading && list.items.length === 0 ? (
        <p className="muted">Memuat…</p>
      ) : list.items.length === 0 ? (
        <p className="muted">Belum ada danmaku.</p>
      ) : (
        <ul className="studio-list">
          {list.items.map((d) => {
            const name = d.author?.display_name || d.author?.username || 'Pengguna'
            return (
              <li key={d.id} className="studio-item">
                <Avatar name={name} url={d.author?.avatar_url} size={36} />
                <div className="studio-item-body">
                  <div className="studio-item-meta">
                    <strong>{name}</strong>
                    <span>{timeAgo(d.created_at)}</span>
                    <span>
                      <span className="danmaku-swatch" style={{ background: d.color }} aria-hidden="true" /> {d.color}
                    </span>
                    <span className="chip">{MODE_LABELS[d.mode] ?? d.mode}</span>
                  </div>
                  <p className="studio-item-text">{d.body}</p>
                  {/* Tautan langsung ke detik danmaku muncul. */}
                  <Link to={`/watch/${d.video_id}?t=${Math.floor(d.position_ms / 1000)}`} className="small">
                    {d.video_title} · {formatDuration(d.position_ms)}
                  </Link>
                </div>
                <button
                  type="button"
                  className="btn btn-small btn-danger-ghost"
                  onClick={() => void remove(d)}
                  disabled={busy !== null}
                  aria-label={`Hapus danmaku dari ${name}`}
                >
                  {busy === d.id ? 'Menghapus…' : 'Hapus'}
                </button>
              </li>
            )
          })}
        </ul>
      )}
      {list.error && list.items.length > 0 && <ErrorBox message={list.error} onRetry={list.retry} />}
      {list.hasMore && !list.error && (
        <button type="button" className="btn load-more" onClick={list.loadMore} disabled={list.loading}>
          {list.loading ? 'Memuat…' : 'Muat lebih banyak'}
        </button>
      )}
    </section>
  )
}
