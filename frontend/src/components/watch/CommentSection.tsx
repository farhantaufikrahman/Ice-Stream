import { useState, type FormEvent, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router'
import { errorMessage } from '../../api/client'
import { createComment, deleteComment, listComments, listReplies } from '../../api/endpoints'
import type { Comment, Profile } from '../../api/types'
import { usePagedList } from '../../hooks/usePagedList'
import { MAX_COMMENT_CHARS } from '../../lib/danmaku'
import { formatCount, timeAgo } from '../../lib/format'
import { charCount } from '../../lib/upload'
import { Avatar } from '../Avatar'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'

interface SectionProps {
  videoId: string
  videoOwnerId: string
  /** comment_count dari data video (komentar + balasan). */
  initialCount: number
  user: Profile | null
}

/**
 * Bagian "Komentar" di halaman tonton (desain Watch.dc.html): tulis
 * komentar, daftar komentar terbaru lebih dulu, balasan satu tingkat, dan
 * hapus (oleh penulis atau pemilik video, aturan dari CommentService).
 * Tombol "Suka" di desain belum ada karena backend belum mendukungnya.
 */
export function CommentSection({ videoId, videoOwnerId, initialCount, user }: SectionProps) {
  const roots = usePagedList<Comment>(async (token, signal) => {
    const page = await listComments(videoId, token, signal)
    return { items: page.comments, next: page.next_page_token }
  })
  // Selisih jumlah komentar karena tulis/hapus di halaman ini.
  const [delta, setDelta] = useState(0)

  return (
    <section className="comments" aria-labelledby="comments-title">
      <h2 id="comments-title" className="comments-title">
        Komentar <span className="muted">{formatCount(Math.max(0, initialCount + delta))}</span>
      </h2>

      {user ? (
        <CommentComposer
          videoId={videoId}
          user={user}
          placeholder="Tulis komentar…"
          onCreated={(c) => {
            roots.update((items) => [c, ...items])
            setDelta((n) => n + 1)
          }}
        />
      ) : (
        <LoginHint what="berkomentar" />
      )}

      {roots.items.map((c) => (
        <CommentThread
          key={c.id}
          comment={c}
          videoId={videoId}
          videoOwnerId={videoOwnerId}
          user={user}
          onCountChange={(n) => setDelta((d) => d + n)}
          onRemoved={() => roots.update((items) => items.filter((x) => x.id !== c.id))}
        />
      ))}

      {roots.error ? (
        <ErrorBox message={roots.error} onRetry={roots.retry} />
      ) : roots.loading ? (
        <p className="muted">Memuat komentar…</p>
      ) : roots.items.length === 0 ? (
        <p className="muted">Belum ada komentar. Jadilah yang pertama!</p>
      ) : roots.hasMore ? (
        <button type="button" className="btn load-more" onClick={roots.loadMore}>
          Muat komentar lainnya
        </button>
      ) : null}
    </section>
  )
}

interface ThreadProps {
  comment: Comment
  videoId: string
  videoOwnerId: string
  user: Profile | null
  /** Jumlah komentar video berubah (+1 balasan baru, -n saat hapus). */
  onCountChange: (n: number) => void
  /** Komentar utama ini sudah dihapus. */
  onRemoved: () => void
}

/** Satu komentar utama beserta balasannya. */
function CommentThread({ comment, videoId, videoOwnerId, user, onCountChange, onRemoved }: ThreadProps) {
  const [expanded, setExpanded] = useState(false)
  const [replyTo, setReplyTo] = useState<{ mention: string } | null>(null)
  // Balasan yang ditulis/dihapus di halaman ini, ditimpakan ke daftar dari server.
  const [added, setAdded] = useState<Comment[]>([])
  const [removed, setRemoved] = useState<ReadonlySet<string>>(new Set())
  const [deleting, setDeleting] = useState<Comment | null>(null)

  const replyCount = Math.max(0, comment.reply_count + added.length - removed.size)

  const startReply = (target: Comment) => {
    const isRoot = target.id === comment.id
    setReplyTo({ mention: isRoot ? '' : `@${target.author?.username ?? ''} ` })
  }

  const onDeleted = (c: Comment) => {
    setDeleting(null)
    if (c.id === comment.id) {
      // Komentar utama dihapus bersama semua balasannya.
      onCountChange(-(1 + replyCount))
      onRemoved()
    } else {
      setRemoved((s) => new Set(s).add(c.id))
      onCountChange(-1)
    }
  }

  return (
    <div className="comment-thread">
      <CommentItem
        comment={comment}
        videoOwnerId={videoOwnerId}
        user={user}
        onReply={user ? () => startReply(comment) : undefined}
        onDelete={() => setDeleting(comment)}
      />

      <div className="comment-children">
        {replyCount > 0 && (
          <button type="button" className="link-btn" onClick={() => setExpanded((v) => !v)} aria-expanded={expanded}>
            {expanded ? 'Sembunyikan balasan' : `Lihat ${replyCount} balasan`}
          </button>
        )}
        {expanded && (
          <Replies
            videoId={videoId}
            rootId={comment.id}
            added={added}
            removed={removed}
            render={(r) => (
              <CommentItem
                key={r.id}
                comment={r}
                videoOwnerId={videoOwnerId}
                user={user}
                onReply={user ? () => startReply(r) : undefined}
                onDelete={() => setDeleting(r)}
              />
            )}
          />
        )}
        {replyTo && user && (
          <CommentComposer
            videoId={videoId}
            parentId={comment.id}
            user={user}
            initialText={replyTo.mention}
            placeholder="Tulis balasan…"
            submitLabel="Balas"
            autoFocus
            onCancel={() => setReplyTo(null)}
            onCreated={(c) => {
              setAdded((list) => [...list, c])
              setExpanded(true)
              setReplyTo(null)
              onCountChange(1)
            }}
          />
        )}
      </div>

      {deleting && (
        <DeleteCommentDialog
          videoId={videoId}
          comment={deleting}
          isRoot={deleting.id === comment.id}
          replyCount={replyCount}
          onClose={() => setDeleting(null)}
          onDeleted={() => onDeleted(deleting)}
        />
      )}
    </div>
  )
}

/** Daftar balasan dari server (terlama lebih dulu) ditambah balasan baru dari halaman ini. */
function Replies({
  videoId,
  rootId,
  added,
  removed,
  render,
}: {
  videoId: string
  rootId: string
  added: Comment[]
  removed: ReadonlySet<string>
  render: (c: Comment) => ReactNode
}) {
  const replies = usePagedList<Comment>(async (token, signal) => {
    const page = await listReplies(videoId, rootId, token, signal)
    return { items: page.comments, next: page.next_page_token }
  })
  const fromServer = new Set(replies.items.map((r) => r.id))
  // Balasan baru selalu paling akhir (urutan terlama lebih dulu), jadi
  // ditampilkan setelah halaman yang sudah dimuat.
  const all = [...replies.items, ...added.filter((a) => !fromServer.has(a.id))].filter((r) => !removed.has(r.id))

  return (
    <div className="replies">
      {all.map(render)}
      {replies.error ? (
        <ErrorBox message={replies.error} onRetry={replies.retry} />
      ) : replies.loading ? (
        <p className="muted small">Memuat balasan…</p>
      ) : replies.hasMore ? (
        <button type="button" className="link-btn" onClick={replies.loadMore}>
          Muat balasan lainnya
        </button>
      ) : null}
    </div>
  )
}

interface ItemProps {
  comment: Comment
  videoOwnerId: string
  user: Profile | null
  onReply?: () => void
  onDelete: () => void
}

function CommentItem({ comment: c, videoOwnerId, user, onReply, onDelete }: ItemProps) {
  const name = c.author?.display_name || c.author?.username || 'Pengguna'
  const canDelete = Boolean(user && (user.id === c.author_id || user.id === videoOwnerId))
  return (
    <article className="comment">
      <Avatar name={name} url={c.author?.avatar_url} size={40} />
      <div className="comment-main">
        <div className="comment-meta">
          <strong>{name}</strong>
          <span className="muted">· {timeAgo(c.created_at)}</span>
          {c.author_id === videoOwnerId && <span className="comment-badge">Kreator</span>}
        </div>
        <p className="comment-body">{c.body}</p>
        {(onReply || canDelete) && (
          <div className="comment-actions">
            {onReply && (
              <button type="button" className="link-btn" onClick={onReply}>
                Balas
              </button>
            )}
            {canDelete && (
              <button type="button" className="link-btn link-danger" onClick={onDelete}>
                Hapus
              </button>
            )}
          </div>
        )}
      </div>
    </article>
  )
}

interface ComposerProps {
  videoId: string
  user: Profile
  parentId?: string
  initialText?: string
  placeholder: string
  submitLabel?: string
  autoFocus?: boolean
  onCancel?: () => void
  onCreated: (c: Comment) => void
}

function CommentComposer({
  videoId,
  user,
  parentId,
  initialText = '',
  placeholder,
  submitLabel = 'Kirim',
  autoFocus,
  onCancel,
  onCreated,
}: ComposerProps) {
  const [text, setText] = useState(initialText)
  const [focused, setFocused] = useState(Boolean(autoFocus))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const count = charCount(text.trim())
  const tooLong = count > MAX_COMMENT_CHARS
  const open = focused || text.length > 0
  const name = user.display_name || user.username

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const body = text.trim()
    if (!body || tooLong) return
    setBusy(true)
    setError(null)
    try {
      const c = await createComment(videoId, body, parentId)
      setText('')
      setFocused(false)
      // Jika profil penulis tidak ikut terkirim (server gagal mengambilnya),
      // pakai profil sendiri supaya nama tetap tampil.
      onCreated(c.author ? c : { ...c, author: { id: user.id, username: user.username, display_name: user.display_name, avatar_url: user.avatar_url } })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const cancel = () => {
    setText('')
    setFocused(false)
    setError(null)
    onCancel?.()
  }

  return (
    <form className="comment-composer" onSubmit={onSubmit}>
      <Avatar name={name} url={user.avatar_url} size={parentId ? 32 : 40} />
      <div className="comment-composer-main">
        <label className="sr-only" htmlFor={`cm-${parentId ?? 'root'}`}>
          {placeholder}
        </label>
        <textarea
          id={`cm-${parentId ?? 'root'}`}
          rows={open ? 3 : 1}
          value={text}
          placeholder={placeholder}
          onChange={(e) => setText(e.target.value)}
          onFocus={() => setFocused(true)}
          autoFocus={autoFocus}
          aria-invalid={tooLong || undefined}
        />
        {error && <span className="field-error">{error}</span>}
        {open && (
          <div className="comment-composer-actions">
            <span className={tooLong ? 'counter is-over' : 'counter'}>
              {count}/{MAX_COMMENT_CHARS}
            </span>
            <span className="spacer" />
            <button type="button" className="btn btn-ghost btn-small" onClick={cancel} disabled={busy}>
              Batal
            </button>
            <button type="submit" className="btn btn-primary btn-small" disabled={busy || !text.trim() || tooLong}>
              {busy ? 'Mengirim…' : submitLabel}
            </button>
          </div>
        )}
      </div>
    </form>
  )
}

function DeleteCommentDialog({
  videoId,
  comment,
  isRoot,
  replyCount,
  onClose,
  onDeleted,
}: {
  videoId: string
  comment: Comment
  isRoot: boolean
  replyCount: number
  onClose: () => void
  onDeleted: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const confirm = async () => {
    setBusy(true)
    setError(null)
    try {
      await deleteComment(videoId, comment.id)
      onDeleted()
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }
  return (
    <Modal title="Hapus komentar?" onClose={onClose} locked={busy}>
      <div className="modal-body">
        <p className="modal-quote">{comment.body}</p>
        {isRoot && replyCount > 0 && <p className="muted">{replyCount} balasan di bawahnya ikut terhapus.</p>}
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="button" className="btn btn-danger" onClick={confirm} disabled={busy}>
            {busy ? 'Menghapus…' : 'Hapus'}
          </button>
        </div>
      </div>
    </Modal>
  )
}

function LoginHint({ what }: { what: string }) {
  const location = useLocation()
  return (
    <p className="muted">
      <Link to={`/login?next=${encodeURIComponent(location.pathname)}`}>Masuk</Link> untuk {what}.
    </p>
  )
}
