import { useState } from 'react'
import { Link } from 'react-router'
import { listMyVideos } from '../api/endpoints'
import type { Video } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { DeleteVideoDialog } from '../components/studio/DeleteVideoDialog'
import { EditVideoDialog } from '../components/studio/EditVideoDialog'
import { StatusChip } from '../components/studio/StatusChip'
import { StudioLayout } from '../components/studio/StudioLayout'
import { ThemeToggle } from '../components/ThemeToggle'
import { Thumbnail } from '../components/Thumbnail'
import { usePagedVideos } from '../hooks/usePagedVideos'
import { useVideoPoll } from '../hooks/useVideoPoll'
import { formatCount, formatDate, formatDuration } from '../lib/format'
import { canDelete, isProcessing } from '../lib/upload'
import { reportReasonLabel } from '../lib/report'
import { videoDate, visibilityLabel } from '../lib/visibility'

type Dialog = { kind: 'edit' | 'delete'; video: Video } | null

/**
 * Creator Studio, menu Konten (/studio/content): semua video milik user,
 * termasuk yang belum READY, dengan tombol Edit dan Hapus. Kartu statistik
 * dan grafik desain Studio.dc.html ada di menu Dasbor (StudioDashboardPage).
 */
export function StudioPage() {
  const { user } = useAuth()
  const paged = usePagedVideos(`mine:${user?.id ?? ''}`, listMyVideos)
  // Perubahan setelah daftar dimuat (edit, hapus, status dari polling)
  // disimpan terpisah lalu ditimpakan ke daftar dari server.
  const [changed, setChanged] = useState<Record<string, Video>>({})
  const [removed, setRemoved] = useState<ReadonlySet<string>>(new Set())
  const [dialog, setDialog] = useState<Dialog>(null)

  const update = (v: Video) => setChanged((m) => ({ ...m, [v.id]: v }))
  const videos = paged.videos.filter((v) => !removed.has(v.id)).map((v) => changed[v.id] ?? v)
  const name = user?.display_name || user?.username || ''

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Konten channel</h1>
          {user && <span className="muted">@{user.username}</span>}
        </div>
        <div className="header-actions">
          <ThemeToggle />
          {user && <Avatar name={name} url={user.avatar_url} size={40} />}
        </div>
      </div>

      <section className="panel">
        <div className="panel-head">
          <h2 className="panel-subtitle">Video saya</h2>
          <Link to="/upload" className="btn btn-primary btn-small">
            + Unggah video
          </Link>
        </div>

        {paged.error && videos.length === 0 ? (
          <ErrorBox message={paged.error} onRetry={paged.retry} />
        ) : paged.loading && videos.length === 0 ? (
          <p className="muted">Memuat…</p>
        ) : videos.length === 0 ? (
          <div className="empty">
            <p>Belum ada video.</p>
            <Link to="/upload" className="btn btn-primary">
              Unggah video pertamamu
            </Link>
          </div>
        ) : (
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
                  <th scope="col" className="num">
                    <span className="sr-only">Aksi</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {videos.map((v) => (
                  <StudioRow
                    key={v.id}
                    video={v}
                    onUpdate={update}
                    onEdit={() => setDialog({ kind: 'edit', video: v })}
                    onDelete={() => setDialog({ kind: 'delete', video: v })}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}

        {paged.error && videos.length > 0 && <ErrorBox message={paged.error} onRetry={paged.retry} />}
        {paged.hasMore && !paged.error && (
          <button type="button" className="btn load-more" onClick={paged.loadMore} disabled={paged.loading}>
            {paged.loading ? 'Memuat…' : 'Muat lebih banyak'}
          </button>
        )}
      </section>

      {dialog?.kind === 'edit' && (
        <EditVideoDialog
          video={dialog.video}
          onClose={() => setDialog(null)}
          onSaved={(v) => {
            update(v)
            setDialog(null)
          }}
          onThumbnailChange={update}
        />
      )}
      {dialog?.kind === 'delete' && (
        <DeleteVideoDialog
          video={dialog.video}
          onClose={() => setDialog(null)}
          onDeleted={(id) => {
            setRemoved((s) => new Set(s).add(id))
            setDialog(null)
          }}
        />
      )}
    </StudioLayout>
  )
}

interface RowProps {
  video: Video
  onUpdate: (v: Video) => void
  onEdit: () => void
  onDelete: () => void
}

function StudioRow({ video: v, onUpdate, onEdit, onDelete }: RowProps) {
  // Video yang sedang di-transcode dicek ulang berkala sampai selesai.
  useVideoPoll(v.id, isProcessing(v.status), onUpdate)
  const ready = v.status === 'READY'
  const deletable = canDelete(v.status)

  return (
    <tr>
      <td>
        <div className="studio-video">
          <div className="studio-thumb">
            <Thumbnail url={v.thumbnail_url} title={v.title} />
            {v.duration_ms ? <span className="duration">{formatDuration(v.duration_ms)}</span> : null}
          </div>
          <div className="studio-video-text">
            {ready ? (
              <Link to={`/watch/${v.id}`} className="studio-video-title">
                {v.title}
              </Link>
            ) : (
              <span className="studio-video-title">{v.title}</span>
            )}
            {v.status === 'FAILED' && v.failure_reason && <span className="small pink">{v.failure_reason}</span>}
            {v.status === 'PENDING_UPLOAD' && <span className="small muted">Unggahan tidak selesai. Hapus lalu unggah ulang.</span>}
            {v.moderation && (
              <span className="small pink">
                Diturunkan admin: {reportReasonLabel(v.moderation.reason)}
                {v.moderation.note && ` · ${v.moderation.note}`}
              </span>
            )}
          </div>
        </div>
      </td>
      <td>
        <div className="chip-row">
          <StatusChip status={v.status} />
          {v.visibility === 'BLOCKED' ? (
            <span className="chip chip-pink">{visibilityLabel(v.visibility)}</span>
          ) : (
            <span className={v.draft ? 'chip chip-amber' : 'chip'}>{v.draft ? 'Draf' : visibilityLabel(v.visibility)}</span>
          )}
        </div>
      </td>
      <td className="nowrap muted">{formatDate(videoDate(v))}</td>
      <td className="num">{ready ? formatCount(v.view_count) : '—'}</td>
      <td className="num">{ready ? formatCount(v.like_count) : '—'}</td>
      <td className="num">{ready ? formatCount(v.comment_count) : '—'}</td>
      <td className="num">
        <div className="row-actions">
          <button type="button" className="btn btn-small" onClick={onEdit} aria-label={`Edit ${v.title}`}>
            Edit
          </button>
          <button
            type="button"
            className="btn btn-small btn-danger-ghost"
            onClick={onDelete}
            disabled={!deletable}
            title={deletable ? undefined : 'Tunggu sampai proses transcoding selesai'}
            aria-label={`Hapus ${v.title}`}
          >
            Hapus
          </button>
        </div>
      </td>
    </tr>
  )
}
