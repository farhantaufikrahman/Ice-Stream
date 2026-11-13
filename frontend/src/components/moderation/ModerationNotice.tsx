import { Link } from 'react-router'
import type { Video } from '../../api/types'
import { formatDate } from '../../lib/format'
import { reportReasonLabel } from '../../lib/report'

/**
 * Pemberitahuan untuk pemilik video yang diturunkan admin (langkah 28).
 * Server hanya mengirim `moderation` ke pemilik, jadi penonton lain tidak
 * pernah melihat ini (mereka mendapat 404).
 */
export function ModerationNotice({ video }: { video: Pick<Video, 'visibility' | 'moderation'> }) {
  if (video.visibility !== 'BLOCKED') return null
  const m = video.moderation
  return (
    <div className="moderation-notice" role="status">
      <strong>Video ini diturunkan admin{m ? ` pada ${formatDate(m.blocked_at)}` : ''}.</strong>
      <span>
        Hanya kamu yang masih bisa membukanya. Video ini tidak tampil di beranda, pencarian, kanal, maupun lewat tautan.
      </span>
      {m && (
        <span>
          Alasan: <strong>{reportReasonLabel(m.reason)}</strong>
          {m.note && <> · {m.note}</>}
        </span>
      )}
      <span className="small">
        Aturannya ada di <Link to="/pedoman-komunitas">Pedoman komunitas</Link>. Hanya admin yang bisa memulihkan video ini.
      </span>
    </div>
  )
}
