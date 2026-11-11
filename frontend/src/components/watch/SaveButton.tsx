import { useState } from 'react'
import { Link, useLocation } from 'react-router'
import { SaveDialog, type SaveState } from './SaveDialog'

interface Props {
  videoId: string
  /** Dari GET /videos/:id: viewer_saved dan viewer_in_playlist. */
  saved: SaveState
  loggedIn: boolean
  disabled?: boolean
}

/**
 * Tombol Simpan di bawah video (desain Watch.dc.html, ikon bookmark).
 * Membuka dialog "Simpan ke..." berisi Tonton nanti dan playlist milik
 * penonton, seperti tombol Save di YouTube. Tamu diarahkan ke halaman
 * Masuk lalu kembali ke video ini.
 */
export function SaveButton({ videoId, saved: initial, loggedIn, disabled = false }: Props) {
  const location = useLocation()
  const [saved, setSaved] = useState(initial)
  const [open, setOpen] = useState(false)

  if (!loggedIn) {
    return (
      <Link to={`/login?next=${encodeURIComponent(location.pathname)}`} className="btn btn-like" title="Masuk untuk menyimpan video">
        <BookmarkIcon filled={false} /> Simpan
      </Link>
    )
  }

  const isSaved = saved.watchLater || saved.inPlaylist
  return (
    <>
      <button
        type="button"
        className={isSaved ? 'btn btn-like is-saved' : 'btn btn-like'}
        onClick={() => setOpen(true)}
        disabled={disabled}
        aria-haspopup="dialog"
        title="Simpan ke Tonton nanti atau playlist"
      >
        <BookmarkIcon filled={isSaved} /> {isSaved ? 'Tersimpan' : 'Simpan'}
      </button>
      {open && <SaveDialog videoId={videoId} initial={saved} onChange={setSaved} onClose={() => setOpen(false)} />}
    </>
  )
}

export function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" />
    </svg>
  )
}
