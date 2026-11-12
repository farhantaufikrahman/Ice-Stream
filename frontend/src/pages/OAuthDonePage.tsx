import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { errorMessage } from '../api/client'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'
import { safeNextPath } from '../lib/format'

/** Pesan untuk kode error dari server (lihat finish() di oauth.go gateway). */
const ERROR_MESSAGES: Record<string, string> = {
  state: 'Sesi masuk sudah kedaluwarsa atau tidak cocok. Silakan coba lagi.',
  denied: 'Kamu membatalkan izin di halaman penyedia.',
  failed: 'Gagal masuk lewat penyedia. Silakan coba lagi sebentar lagi.',
  email_unverified: 'Email akunmu di penyedia belum terverifikasi. Verifikasi dulu di sana, lalu coba lagi.',
  email_needs_password:
    'Email ini sudah dipakai akun Ice-Stream yang belum terverifikasi. Masuk dengan kata sandi, atau pakai "Lupa kata sandi?" untuk membuktikan email ini milikmu, lalu coba lagi.',
  already_linked: 'Akun Ice-Stream dengan email ini sudah tersambung ke akun lain di penyedia yang sama.',
  banned: 'Akun ini diblokir.',
}

/**
 * /oauth/selesai (langkah 26): halaman tujuan setelah login Google/GitHub.
 *
 * Server mengirim hasilnya di fragment (#code=...&next=... atau #error=...).
 * Fragment tidak pernah dikirim browser ke server mana pun dan tidak masuk
 * header Referer, jadi kode sekali pakai tidak bocor ke log.
 */
export function OAuthDonePage() {
  const { loginWithCode } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  // Dibaca sekali saat halaman dibuka; fragment langsung dihapus dari URL.
  const [frag] = useState(() => new URLSearchParams(location.hash.slice(1)))
  const code = frag.get('code') ?? ''
  const next = safeNextPath(frag.get('next'))
  const errCode = frag.get('error') ?? (code ? '' : 'failed')
  const [error, setError] = useState<string | null>(errCode ? (ERROR_MESSAGES[errCode] ?? ERROR_MESSAGES.failed) : null)
  // StrictMode menjalankan effect dua kali, padahal kode hanya bisa ditukar sekali.
  const started = useRef(false)

  useEffect(() => {
    // Hapus kode dari address bar dan riwayat browser.
    window.history.replaceState(null, '', window.location.pathname)
    if (!code || started.current) return
    started.current = true
    const run = async () => {
      try {
        await loginWithCode(code)
        navigate(next, { replace: true })
      } catch (err) {
        setError(errorMessage(err))
      }
    }
    void run()
  }, [code, next, loginWithCode, navigate])

  return (
    <div className="auth-card">
      <h2>{error ? 'Gagal masuk' : 'Sedang masuk…'}</h2>
      {error ? (
        <>
          <ErrorBox message={error} />
          <p className="muted">
            <Link to={`/login?next=${encodeURIComponent(next)}`}>Kembali ke halaman Masuk</Link>
          </p>
        </>
      ) : (
        <p className="muted">Menyiapkan akunmu, tunggu sebentar.</p>
      )}
    </div>
  )
}
