import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router'
import { useAuth } from '../../auth/useAuth'
import { Avatar } from '../Avatar'
import { LogoMark, Wordmark } from '../Logo'
import { ThemeToggle } from '../ThemeToggle'

/**
 * Kerangka Creator Studio (desain Studio.dc.html): menu di kiri, isi di
 * kanan, dengan urutan menu sama seperti desain.
 */
export function StudioLayout({ children }: { children: ReactNode }) {
  return (
    <div className="studio">
      <nav className="studio-nav" aria-label="Menu Creator Studio">
        <Link to="/" className="brand studio-brand" aria-label="Beranda Ice-Stream">
          <LogoMark size={34} />
          <span className="studio-brand-text">
            <Wordmark size={19} />
            <span className="studio-brand-sub">Creator Studio</span>
          </span>
        </Link>
        <NavLink to="/studio" end className="studio-link">
          Dasbor
        </NavLink>
        <NavLink to="/studio/content" className="studio-link">
          Konten
        </NavLink>
        <NavLink to="/studio/live" className="studio-link">
          Siaran langsung
        </NavLink>
        <NavLink to="/studio/analytics" className="studio-link">
          Analitik
        </NavLink>
        <NavLink to="/studio/comments" className="studio-link">
          Komentar
        </NavLink>
        <NavLink to="/studio/danmaku" className="studio-link">
          Danmaku
        </NavLink>
        <NavLink to="/studio/settings" className="studio-link">
          Pengaturan
        </NavLink>
        <Link to="/" className="studio-link">
          Kembali ke beranda
        </Link>
        <Link to="/upload" className="btn btn-primary studio-upload">
          + Unggah video
        </Link>
      </nav>
      <main className="studio-main">{children}</main>
    </div>
  )
}

/** Header halaman Unggah (desain Upload.dc.html): logo, breadcrumb, tema, avatar. */
export function StudioHeader({ current }: { current: string }) {
  const { user } = useAuth()
  const name = user?.display_name || user?.username || ''
  return (
    <header className="header">
      <Link to="/" className="brand" aria-label="Beranda Ice-Stream">
        <LogoMark size={36} />
        <Wordmark size={22} />
      </Link>
      <nav aria-label="Breadcrumb" className="crumbs">
        <Link to="/studio">Creator Studio</Link> / <span aria-current="page">{current}</span>
      </nav>
      <span className="spacer" />
      <div className="header-actions">
        <ThemeToggle />
        {user && <Avatar name={name} url={user.avatar_url} size={40} />}
      </div>
    </header>
  )
}

/** Ikon panah ke atas (tombol Unggah di header desain). */
export function UploadIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="17 8 12 3 7 8" />
      <line x1="12" y1="3" x2="12" y2="15" />
    </svg>
  )
}
