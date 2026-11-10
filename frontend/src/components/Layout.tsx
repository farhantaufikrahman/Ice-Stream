import { useState, type FormEvent } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router'
import { useAuth } from '../auth/useAuth'
import { Avatar } from './Avatar'

/** Kerangka semua halaman: header di atas, isi halaman di <Outlet />. */
export function Layout() {
  return (
    <>
      <Header />
      <main className="page">
        <Outlet />
      </main>
    </>
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
      <Link to="/" className="logo">
        <span className="logo-mark" aria-hidden="true">▶</span> Ice-Stream
      </Link>
      {/* key: isi kotak ikut berganti saat URL ?q= berubah (misalnya tombol Back). */}
      <form className="search" role="search" onSubmit={onSearch} key={currentQuery}>
        <input name="q" type="search" placeholder="Cari video" defaultValue={currentQuery} aria-label="Cari video" />
        <button type="submit" className="btn">
          Cari
        </button>
      </form>
      <nav className="header-actions">
        {status === 'authenticated' && user ? (
          <UserMenu name={user.display_name || user.username} avatarUrl={user.avatar_url} onLogout={logout} />
        ) : status === 'anonymous' ? (
          <>
            <NavLink to={`/login?next=${next}`} className="btn btn-ghost">
              Masuk
            </NavLink>
            <NavLink to="/register" className="btn btn-primary">
              Daftar
            </NavLink>
          </>
        ) : null}
      </nav>
    </header>
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
      <Avatar name={name} url={avatarUrl} size={32} />
      <span className="user-name">{name}</span>
      <button type="button" className="btn btn-ghost" onClick={handleLogout} disabled={busy}>
        Keluar
      </button>
    </div>
  )
}
