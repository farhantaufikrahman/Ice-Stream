import { useId, useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { reportVideo } from '../../api/endpoints'
import { MAX_REPORT_DETAILS, REPORT_REASONS, reportDetailsError, type ReportReason } from '../../lib/report'
import { charCount } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'

interface Props {
  videoId: string
  title: string
  onClose: () => void
}

/**
 * Dialog Laporkan (langkah 24b). Laporan disimpan di server untuk
 * ditinjau; videonya tidak langsung disembunyikan. Satu laporan per orang
 * per video: mengirim lagi mengganti laporan sebelumnya.
 */
export function ReportDialog({ videoId, title, onClose }: Props) {
  const [reason, setReason] = useState<ReportReason | null>(null)
  const [details, setDetails] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sent, setSent] = useState(false)
  const detailsId = useId()

  const count = charCount(details.trim())
  const detailsError = reportDetailsError(reason, details, count)
  const canSend = reason !== null && !detailsError && !busy

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!reason || !canSend) return
    setBusy(true)
    setError(null)
    try {
      await reportVideo(videoId, reason, details.trim())
      setSent(true)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  if (sent) {
    return (
      <Modal title="Laporan terkirim" onClose={onClose}>
        <div className="modal-body">
          <p>Terima kasih. Laporanmu sudah kami terima dan akan ditinjau.</p>
          <p className="muted small">Videonya tidak langsung disembunyikan. Kalau kamu melapor lagi, laporan ini akan diganti.</p>
          <div className="form-actions">
            <button type="button" className="btn btn-primary" onClick={onClose}>
              Tutup
            </button>
          </div>
        </div>
      </Modal>
    )
  }

  return (
    <Modal title="Laporkan video" onClose={onClose} locked={busy}>
      <form className="modal-body" onSubmit={submit} noValidate>
        <p className="modal-video-title">{title}</p>
        <fieldset className="field visibility-field" disabled={busy}>
          <legend>Apa masalahnya?</legend>
          <div className="report-reasons">
            {REPORT_REASONS.map((r) => (
              <label key={r.value} className={reason === r.value ? 'radio-card is-active' : 'radio-card'}>
                <input type="radio" name="report-reason" value={r.value} checked={reason === r.value} onChange={() => setReason(r.value)} />
                <span className="radio-card-label">{r.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field">
          <div className="field-head">
            <label htmlFor={detailsId}>Keterangan {reason === 'other' ? '(wajib)' : '(opsional)'}</label>
            <span className={count > MAX_REPORT_DETAILS ? 'counter is-over' : 'counter'}>
              {count}/{MAX_REPORT_DETAILS}
            </span>
          </div>
          <textarea
            id={detailsId}
            rows={3}
            value={details}
            onChange={(e) => setDetails(e.target.value)}
            placeholder="Misalnya menit ke berapa, atau tautan karya asli untuk hak cipta"
            disabled={busy}
            aria-invalid={reason !== null && detailsError !== null}
          />
          {reason !== null && detailsError && <span className="field-error">{detailsError}</span>}
        </div>
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="submit" className="btn btn-danger" disabled={!canSend}>
            {busy ? 'Mengirim…' : 'Kirim laporan'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
