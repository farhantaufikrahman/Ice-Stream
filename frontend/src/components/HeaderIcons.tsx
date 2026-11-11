import { NavLink } from 'react-router'
import { badgeText } from '../lib/format'
import { useUnread } from '../notifications/useUnread'

/**
 * Tombol lonceng di header (desain Home.dc.html): kotak 44px, titik
 * stroberi jika ada yang belum dibaca. Di sini titiknya berisi angka.
 */
export function NotificationBell() {
  const { count } = useUnread()
  const label = count > 0 ? `Notifikasi, ${badgeText(count)} belum dibaca` : 'Notifikasi'
  return (
    <NavLink to="/notifications" className="icon-btn header-icon" aria-label={label} title={label}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
        <path d="M13.7 21a2 2 0 0 1-3.4 0" />
      </svg>
      {count > 0 && (
        <span className="badge" aria-hidden="true">
          {badgeText(count)}
        </span>
      )}
    </NavLink>
  )
}

/** Tombol Riwayat tonton di header, gaya sama dengan lonceng. */
export function HistoryLink() {
  return (
    <NavLink to="/history" className="icon-btn header-icon" aria-label="Riwayat tonton" title="Riwayat tonton">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
        <path d="M3 3v5h5" />
        <path d="M12 7v5l3 2" />
      </svg>
    </NavLink>
  )
}

/** Tombol Tonton nanti di header (ikon bookmark yang sama dengan tombol Simpan). */
export function WatchLaterLink() {
  return (
    <NavLink to="/watch-later" className="icon-btn header-icon" aria-label="Tonton nanti" title="Tonton nanti">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z" />
      </svg>
    </NavLink>
  )
}
