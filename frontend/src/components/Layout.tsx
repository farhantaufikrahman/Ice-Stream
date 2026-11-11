import { useState, type FormEvent } from 'react'
import { Link, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router'
import { useAuth } from '../auth/useAuth'
import { Avatar } from './Avatar'
import { UploadIcon } from './studio/StudioLayout'
import { LogoMark, Wordmark } from './Logo'
import { ThemeToggle } from './ThemeToggle'

/** Kerangka halaman: header, isi halaman (<Outlet />), lalu footer. */
export function Layout() {
  return (
    <div className="app">
      <Header />
      <main className="page">
        <Outlet />
      </main>
      <footer className="footer">
        <span>© 2026 Ice-Stream</span>
      </footer>
    </div>
  )
}

function Header() {
  const { status, user, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const currentQuery = location.pathname === '/search' ? (params.get('q') ?? '') : ''

  const onSearch = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const q = String(new FormData(e.currentTarget).get('q') ?? '').trim()
    if (q) navigate(`/search?q=${encodeURIComponent(q)}`)
  }

  const next = encodeURIComponent(location.pathname + location.search)
  return (
    <header className="header">
      <Link to="/" className="brand" aria-label="Beranda Ice-Stream">
        <LogoMark size={38} />
        <Wordmark size={22} />
      </Link>
      {/* key: isi kotak ikut berganti saat URL ?q= berubah (misalnya tombol Back). */}
      <form className="search" role="search" onSubmit={onSearch} key={currentQuery}>
        <label htmlFor="q" className="sr-only">
          Cari video
        </label>
        <SearchIcon />
        <input id="q" name="q" type="search" placeholder="Cari video" defaultValue={currentQuery} />
        <button type="submit" className="btn btn-primary btn-small">
          Cari
        </button>
      </form>
      <div className="header-actions">
        {status === 'authenticated' && (
          <Link to="/upload" className="btn btn-primary" aria-label="Unggah video">
            <UploadIcon />
            <span className="hide-narrow">Unggah</span>
          </Link>
        )}
        <ThemeToggle />
        {status === 'authenticated' && user ? (
          <UserMenu name={user.display_name || user.username} avatarUrl={user.avatar_url} onLogout={logout} />
        ) : status === 'anonymous' ? (
          <>
            <Link to={`/login?next=${next}`} className="btn btn-ghost">
              Masuk
            </Link>
            <Link to="/register" className="btn btn-amber">
              Daftar
            </Link>
          </>
        ) : null}
      </div>
    </header>
  )
}

function SearchIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  )
}

function UserMenu({ name, avatarUrl, onLogout }: { name: string; avatarUrl: string; onLogout: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()
  const handleLogout = async () => {
    setBusy(true)
    try {
      await onLogout()
    } finally {
      setBusy(false)
      navigate('/')
    }
  }
  return (
    <div className="user-menu">
      <Link to="/studio" className="user-link" title="Creator Studio">
        <Avatar name={name} url={avatarUrl} size={40} />
        <span className="user-name">{name}</span>
      </Link>
      <button type="button" className="btn btn-ghost" onClick={handleLogout} disabled={busy}>
        Keluar
      </button>
    </div>
  )
}
