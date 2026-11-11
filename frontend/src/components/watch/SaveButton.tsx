import { useState } from 'react'
import { Link, useLocation } from 'react-router'
import { errorMessage } from '../../api/client'
import { setWatchLater } from '../../api/endpoints'
import { ErrorBox } from '../ErrorBox'

interface Props {
  videoId: string
  /** Dari GET /videos/:id (viewer_saved). */
  saved: boolean
  loggedIn: boolean
  disabled?: boolean
}

/**
 * Tombol Simpan di bawah video (desain Watch.dc.html, ikon bookmark):
 * menyimpan atau mengeluarkan video dari Tonton nanti. Tamu diarahkan ke
 * halaman Masuk lalu kembali ke video ini.
 */
export function SaveButton({ videoId, saved: initial, loggedIn, disabled = false }: Props) {
  const location = useLocation()
  const [saved, setSaved] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!loggedIn) {
    return (
      <Link to={`/login?next=${encodeURIComponent(location.pathname)}`} className="btn btn-like" title="Masuk untuk menyimpan video">
        <BookmarkIcon filled={false} /> Simpan
      </Link>
    )
  }

  const toggle = async () => {
    const want = !saved
    // Optimistic update, sama seperti tombol Suka: tampilan langsung
    // berubah, lalu dikembalikan jika server menolak.
    setSaved(want)
    setBusy(true)
    setError(null)
    try {
      const r = await setWatchLater(videoId, want)
      setSaved(r.saved)
    } catch (err) {
      setSaved(!want)
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <button
        type="button"
        className={saved ? 'btn btn-like is-saved' : 'btn btn-like'}
        onClick={toggle}
        disabled={busy || disabled}
        aria-pressed={saved}
        title={saved ? 'Keluarkan dari Tonton nanti' : 'Simpan ke Tonton nanti'}
      >
        <BookmarkIcon filled={saved} /> {saved ? 'Tersimpan' : 'Simpan'}
      </button>
      {error && <ErrorBox message={error} />}
    </>
  )
}

function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" />
    </svg>
  )
}
