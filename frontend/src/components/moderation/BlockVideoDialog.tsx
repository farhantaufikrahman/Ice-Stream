import { useId, useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { blockVideo } from '../../api/endpoints'
import type { BlockResult } from '../../api/types'
import { MAX_REPORT_DETAILS, REPORT_REASONS, reportDetailsError, type ReportReason } from '../../lib/report'
import { charCount } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'

interface Props {
  videoId: string
  title: string
  /** Alasan terbanyak di laporan, dipilih lebih dulu supaya admin cukup menekan Turunkan. */
  suggested?: ReportReason
  onClose: () => void
  onBlocked: (result: BlockResult) => void
}

/**
 * Dialog "Turunkan video" (langkah 28). Catatan ikut dikirim ke pemilik
 * video di halaman tonton, jadi tulis untuk dibaca kreator. Aturannya sama
 * dengan server: maksimal 500 karakter, wajib untuk alasan "Lainnya".
 */
export function BlockVideoDialog({ videoId, title, suggested, onClose, onBlocked }: Props) {
  const [reason, setReason] = useState<ReportReason | null>(suggested ?? null)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const noteId = useId()

  const count = charCount(note.trim())
  const noteError = reportDetailsError(reason, note, count)
  const canSend = reason !== null && !noteError && !busy

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!reason || !canSend) return
    setBusy(true)
    setError(null)
    try {
      onBlocked(await blockVideo(videoId, reason, note.trim()))
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title="Turunkan video" onClose={onClose} locked={busy}>
      <form className="modal-body" onSubmit={submit} noValidate>
        <p className="modal-video-title">{title}</p>
        <p className="muted small">
          Video langsung hilang dari beranda, pencarian, kanal, dan tautannya. Semua laporan terbuka ikut ditutup, dan pemilik video mendapat
          notifikasi beserta alasan dan catatan ini.
        </p>
        <fieldset className="field visibility-field" disabled={busy}>
          <legend>Alasan</legend>
          <div className="report-reasons">
            {REPORT_REASONS.map((r) => (
              <label key={r.value} className={reason === r.value ? 'radio-card is-active' : 'radio-card'}>
                <input type="radio" name="block-reason" value={r.value} checked={reason === r.value} onChange={() => setReason(r.value)} />
                <span className="radio-card-label">{r.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field">
          <div className="field-head">
            <label htmlFor={noteId}>Catatan untuk kreator {reason === 'other' ? '(wajib)' : '(opsional)'}</label>
            <span className={count > MAX_REPORT_DETAILS ? 'counter is-over' : 'counter'}>
              {count}/{MAX_REPORT_DETAILS}
            </span>
          </div>
          <textarea
            id={noteId}
            rows={3}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Misalnya bagian mana yang melanggar"
            disabled={busy}
            aria-invalid={reason !== null && noteError !== null}
          />
          {reason !== null && noteError && <span className="field-error">{noteError}</span>}
        </div>
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="submit" className="btn btn-danger" disabled={!canSend}>
            {busy ? 'Menurunkan…' : 'Turunkan'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
