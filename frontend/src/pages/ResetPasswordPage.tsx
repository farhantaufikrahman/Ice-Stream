import { useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router'
import { ApiError, errorMessage } from '../api/client'
import { resetPassword } from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'

/**
 * Langkah 24c: halaman dari tautan di email,
 * /reset-password?token=... Aturan kata sandi diperiksa AuthService; di
 * sini hanya dicek bahwa kedua isian sama.
 */
export function ResetPasswordPage() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''
  const { status, logout } = useAuth()
  const [state, setState] = useState<'form' | 'done' | 'expired'>('form')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const password = String(form.get('password'))
    if (password !== String(form.get('confirm'))) {
      setError('Kedua kata sandi tidak sama.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await resetPassword(token, password)
      // Semua sesi sudah dicabut server. Jika tab ini sedang login,
      // keluarkan juga supaya tidak tampak masih masuk.
      if (status === 'authenticated') {
        await logout().catch(() => undefined)
      }
      setState('done')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'FAILED_PRECONDITION') {
        setState('expired')
      } else {
        setError(errorMessage(err))
      }
    } finally {
      setBusy(false)
    }
  }

  if (!token || state === 'expired') {
    return (
      <div className="auth-card">
        <h2>Tautan tidak berlaku</h2>
        <p className="muted">
          {token
            ? 'Tautan ini sudah dipakai, sudah kedaluwarsa, atau sudah diganti tautan yang lebih baru.'
            : 'Tautan ini tidak lengkap. Buka lagi tautan dari email, atau minta tautan baru.'}
        </p>
        <Link to="/forgot-password" className="btn btn-primary btn-block">
          Minta tautan baru
        </Link>
      </div>
    )
  }

  if (state === 'done') {
    return (
      <div className="auth-card">
        <h2>Kata sandi diganti</h2>
        <p className="muted">
          Kata sandi barumu sudah aktif. Demi keamanan, semua perangkat yang sedang masuk sudah dikeluarkan.
        </p>
        <Link to="/login" className="btn btn-primary btn-block">
          Masuk
        </Link>
      </div>
    )
  }

  return (
    <form className="auth-card" onSubmit={onSubmit}>
      <h2>Buat kata sandi baru</h2>
      <p className="muted">Minimal 8 karakter. Setelah diganti, semua perangkat harus masuk lagi.</p>
      {error && <ErrorBox message={error} />}
      <label>
        Kata sandi baru
        <input name="password" type="password" autoComplete="new-password" required autoFocus />
      </label>
      <label>
        Ulangi kata sandi baru
        <input name="confirm" type="password" autoComplete="new-password" required />
      </label>
      <button type="submit" className="btn btn-primary btn-block" disabled={busy}>
        {busy ? 'Menyimpan…' : 'Simpan kata sandi'}
      </button>
      <p className="muted">
        Batal? <Link to="/login">Kembali ke Masuk</Link>
      </p>
    </form>
  )
}
