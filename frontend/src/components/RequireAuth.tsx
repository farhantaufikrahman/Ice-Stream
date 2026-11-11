import { Navigate, Outlet, useLocation } from 'react-router'
import { useAuth } from '../auth/useAuth'

/**
 * Pembungkus halaman yang wajib login (Studio, Unggah). Tamu dialihkan ke
 * /login?next=... lalu kembali ke halaman ini setelah masuk.
 * Mirip middleware requireAuth di Express, tetapi hanya untuk tampilan:
 * keamanan sebenarnya tetap dicek server (RequireAuth di gateway).
 */
export function RequireAuth() {
  const { status } = useAuth()
  const location = useLocation()
  if (status === 'loading') {
    return <p className="muted center">Memuat…</p>
  }
  if (status === 'anonymous') {
    const next = encodeURIComponent(location.pathname + location.search)
    return <Navigate to={`/login?next=${next}`} replace />
  }
  return <Outlet />
}
