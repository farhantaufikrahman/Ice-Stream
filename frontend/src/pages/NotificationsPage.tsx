import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { listNotifications, markAllNotificationsRead, markNotificationRead } from '../api/endpoints'
import type { AppNotification } from '../api/types'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedList } from '../hooks/usePagedList'
import { timeAgo } from '../lib/format'
import { reportReasonLabel } from '../lib/report'
import { useUnread } from '../notifications/useUnread'

/**
 * Halaman Notifikasi (/notifications). Jenisnya:
 * - NEW_VIDEO: kanal yang kamu ikuti mengunggah video baru (dibuat di
 *   background lewat NATS JetStream saat video selesai diproses).
 * - VIDEO_BLOCKED / VIDEO_RESTORED (langkah 28): video milikmu diturunkan
 *   atau dipulihkan admin.
 */
export function NotificationsPage() {
  const { count, setCount, refresh } = useUnread()
  const list = usePagedList<AppNotification>((token, signal) =>
    listNotifications(token, signal).then((p) => ({ items: p.notifications, next: p.next_page_token })),
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Samakan angka di lonceng dengan server saat halaman dibuka.
  useEffect(() => refresh(), [refresh])

  const hasUnread = count > 0 || list.items.some((n) => !n.read)

  const markOne = (n: AppNotification) => {
    if (n.read) return
    list.update((items) => items.map((x) => (x.id === n.id ? { ...x, read: true } : x)))
    // Tidak ditunggu: halaman langsung pindah ke video. Angka di lonceng
    // dibetulkan dengan jawaban server.
    markNotificationRead(n.id)
      .then((r) => setCount(r.unread_count))
      .catch(() => refresh())
  }

  const markAll = async () => {
    setBusy(true)
    setError(null)
    try {
      const r = await markAllNotificationsRead()
      list.update((items) => items.map((x) => ({ ...x, read: true })))
      setCount(r.unread_count)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="narrow-page">
      <div className="list-page-head">
        <h1 className="page-title">Notifikasi</h1>
        <button type="button" className="btn btn-small" onClick={markAll} disabled={busy || !hasUnread}>
          {busy ? 'Menandai…' : 'Tandai semua sudah dibaca'}
        </button>
      </div>
      {error && <ErrorBox message={error} />}

      {list.items.length > 0 && (
        <ul className="notif-list">
          {list.items.map((n) => (
            <li key={n.id}>
              <NotificationRow n={n} onOpen={() => markOne(n)} />
            </li>
          ))}
        </ul>
      )}
      {!list.loading && !list.error && list.items.length === 0 && (
        <div className="empty">
          <p>Belum ada notifikasi.</p>
          <p className="muted small">Ikuti kanal favoritmu: saat mereka mengunggah video baru, kabarnya muncul di sini.</p>
        </div>
      )}
      {list.error && <ErrorBox message={list.error} onRetry={list.retry} />}
      {list.loading && <p className="muted center">Memuat…</p>}
      {!list.loading && !list.error && list.hasMore && (
        <div className="center">
          <button type="button" className="btn" onClick={list.loadMore}>
            Muat lagi
          </button>
        </div>
      )}
    </div>
  )
}

function NotificationRow({ n, onOpen }: { n: AppNotification; onOpen: () => void }) {
  if (n.kind === 'VIDEO_BLOCKED' || n.kind === 'VIDEO_RESTORED') return <ModerationRow n={n} onOpen={onOpen} />
  const owner = n.video.owner
  const name = owner?.display_name || owner?.username || 'Kanal yang kamu ikuti'
  return (
    <Link to={`/watch/${n.video.id}`} className={n.read ? 'notif' : 'notif is-unread'} onClick={onOpen}>
      <span className="notif-dot" aria-hidden="true" />
      <Avatar name={name} url={owner?.avatar_url} size={44} />
      <span className="notif-text">
        <span>
          <strong>{name}</strong> mengunggah video baru
          {!n.read && <span className="sr-only"> (belum dibaca)</span>}
        </span>
        <span className="notif-title">{n.video.title}</span>
        <span className="muted small">{timeAgo(n.created_at)}</span>
      </span>
      <span className="notif-thumb">
        <Thumbnail url={n.video.thumbnail_url} title={n.video.title} />
      </span>
    </Link>
  )
}

/** Notifikasi moderasi untuk pemilik video (langkah 28). */
function ModerationRow({ n, onOpen }: { n: AppNotification; onOpen: () => void }) {
  const blocked = n.kind === 'VIDEO_BLOCKED'
  return (
    <Link to={`/watch/${n.video.id}`} className={n.read ? 'notif' : 'notif is-unread'} onClick={onOpen}>
      <span className="notif-dot" aria-hidden="true" />
      <span className={blocked ? 'notif-icon is-blocked' : 'notif-icon'} aria-hidden="true">
        {blocked ? '!' : '✓'}
      </span>
      <span className="notif-text">
        <span>
          {blocked ? (
            <>
              Videomu <strong>diturunkan admin</strong>
              {n.moderation_reason && <> karena {reportReasonLabel(n.moderation_reason).toLowerCase()}</>}
            </>
          ) : (
            <>
              Videomu <strong>dipulihkan admin</strong> dan bisa ditonton lagi
            </>
          )}
          {!n.read && <span className="sr-only"> (belum dibaca)</span>}
        </span>
        <span className="notif-title">{n.video.title}</span>
        <span className="muted small">{timeAgo(n.created_at)}</span>
      </span>
      <span className="notif-thumb">
        <Thumbnail url={n.video.thumbnail_url} title={n.video.title} />
      </span>
    </Link>
  )
}
