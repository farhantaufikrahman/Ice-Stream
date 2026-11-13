import { useId, useRef, useState } from 'react'
import { formatDuration } from '../../lib/format'
import { liveShareUrl, shareUrl } from '../../lib/share'
import { Modal } from '../Modal'

/** Yang dibagikan: video (bisa "mulai di detik ke-N") atau siaran langsung. */
export type ShareTarget =
  | {
      videoId: string
      /** Detik video saat tombol Bagikan ditekan; 0 jika belum diputar. */
      currentSeconds: number
    }
  | { liveId: string }

interface Props {
  target: ShareTarget
  title: string
  onClose: () => void
}

type CopyState = 'idle' | 'copied' | 'failed'

/**
 * Dialog Bagikan (langkah 24b; siaran langsung sejak langkah 25c): tautan
 * video, pilihan "Mulai di 1:23", dan
 * tombol Salin. Di HP yang mendukung Web Share API ada tombol untuk
 * membuka menu bagikan bawaan (WhatsApp, Telegram, dan lainnya).
 *
 * Clipboard API: https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText
 * Web Share API: https://developer.mozilla.org/en-US/docs/Web/API/Navigator/share
 */
export function ShareDialog({ target, title, onClose }: Props) {
  // Siaran langsung tidak punya pilihan "Mulai di": penonton selalu masuk
  // ke bagian yang sedang tayang.
  const start = 'videoId' in target ? Math.floor(target.currentSeconds) : 0
  const [withStart, setWithStart] = useState(false)
  const [copy, setCopy] = useState<CopyState>('idle')
  const inputId = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const url = 'videoId' in target ? shareUrl(window.location.origin, target.videoId, withStart ? start : 0) : liveShareUrl(window.location.origin, target.liveId)
  const canShare = typeof navigator.share === 'function'

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(url)
      setCopy('copied')
    } catch {
      // Clipboard bisa ditolak (misalnya situs dibuka lewat http biasa di
      // alamat selain localhost). Teks tautan dipilih supaya bisa disalin
      // manual dengan Ctrl+C.
      inputRef.current?.focus()
      inputRef.current?.select()
      setCopy('failed')
    }
  }

  const shareNative = () => {
    // Ditutup sendiri oleh user (AbortError) bukan kesalahan: abaikan.
    navigator.share({ title, url }).catch(() => {})
  }

  return (
    <Modal title={'videoId' in target ? 'Bagikan video' : 'Bagikan siaran'} onClose={onClose}>
      <div className="modal-body">
        <div className="field">
          <label htmlFor={inputId}>Tautan</label>
          <div className="share-row">
            <input
              id={inputId}
              ref={inputRef}
              className="share-url"
              type="text"
              value={url}
              readOnly
              onFocus={(e) => e.currentTarget.select()}
            />
            <button type="button" className="btn btn-primary" onClick={copyLink}>
              {copy === 'copied' ? 'Tersalin' : 'Salin'}
            </button>
          </div>
          <span className="muted small" role="status">
            {copy === 'copied' && 'Tautan sudah disalin.'}
            {copy === 'failed' && 'Browser menolak menyalin otomatis. Tautan sudah dipilih, tekan Ctrl+C.'}
          </span>
        </div>
        {start > 0 && (
          <label className="check-row">
            <input
              type="checkbox"
              checked={withStart}
              onChange={(e) => {
                setWithStart(e.target.checked)
                setCopy('idle')
              }}
            />
            Mulai di {formatDuration(start * 1000)}
          </label>
        )}
        <div className="form-actions">
          {canShare && (
            <button type="button" className="btn" onClick={shareNative}>
              Bagikan lewat aplikasi lain
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>
            Tutup
          </button>
        </div>
      </div>
    </Modal>
  )
}
