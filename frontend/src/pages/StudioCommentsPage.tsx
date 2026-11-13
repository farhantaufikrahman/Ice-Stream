import { useCallback, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { errorMessage } from '../api/client'
import { deleteComment, listMyComments } from '../api/endpoints'
import type { ChannelComment } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { StudioLayout } from '../components/studio/StudioLayout'
import { VideoFilter } from '../components/studio/VideoFilter'
import { ThemeToggle } from '../components/ThemeToggle'
import { useMyVideoOptions } from '../hooks/useMyVideoOptions'
import { usePagedList } from '../hooks/usePagedList'
import { timeAgo } from '../lib/format'

/**
 * Creator Studio, menu Komentar (langkah 26): komentar dan balasan di semua
 * video milik user, terbaru lebih dulu, dengan tombol Hapus (pemilik video
 * boleh menghapus komentar siapa pun di videonya).
 */
export function StudioCommentsPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const videoId = params.get('video') ?? ''
  const options = useMyVideoOptions(user?.id ?? '')

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Komentar</h1>
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
      <CommentList key={videoId} videoId={videoId} />
    </StudioLayout>
  )
}

function CommentList({ videoId }: { videoId: string }) {
  const fetchPage = useCallback(
    async (token: string, signal: AbortSignal) => {
      const page = await listMyComments(videoId, token, signal)
      return { items: page.comments, next: page.next_page_token }
    },
    [videoId],
  )
  const list = usePagedList<ChannelComment>(fetchPage)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const remove = async (c: ChannelComment) => {
    if (!window.confirm('Hapus komentar ini? Balasannya ikut tersembunyi.')) return
    setBusy(c.id)
    setError(null)
    try {
      await deleteComment(c.video_id, c.id)
      // Komentar utama yang dihapus juga menyembunyikan balasannya.
      list.update((items) => items.filter((x) => x.id !== c.id && x.parent_id !== c.id))
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
        <p className="muted">Belum ada komentar.</p>
      ) : (
        <ul className="studio-list">
          {list.items.map((c) => {
            const name = c.author?.display_name || c.author?.username || 'Pengguna'
            return (
              <li key={c.id} className="studio-item">
                <Avatar name={name} url={c.author?.avatar_url} size={36} />
                <div className="studio-item-body">
                  <div className="studio-item-meta">
                    <strong>{name}</strong>
                    <span>{timeAgo(c.created_at)}</span>
                    {c.parent_id && <span className="chip">Balasan</span>}
                  </div>
                  <p className="studio-item-text">{c.body}</p>
                  <Link to={`/watch/${c.video_id}`} className="small">
                    {c.video_title}
                  </Link>
                </div>
                <button
                  type="button"
                  className="btn btn-small btn-danger-ghost"
                  onClick={() => void remove(c)}
                  disabled={busy !== null}
                  aria-label={`Hapus komentar dari ${name}`}
                >
                  {busy === c.id ? 'Menghapus…' : 'Hapus'}
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
