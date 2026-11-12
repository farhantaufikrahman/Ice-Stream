import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router'
import { errorMessage } from '../api/client'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'
import { safeNextPath } from '../lib/format'

export function LoginPage() {
  const { status, login } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = safeNextPath(params.get('next'))
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (status === 'authenticated' && !busy) {
    return <Navigate to={next} replace />
  }

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setBusy(true)
    setError(null)
    try {
      await login(String(form.get('login')), String(form.get('password')))
      navigate(next, { replace: true })
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <form className="auth-card" onSubmit={onSubmit}>
      <h2>Masuk</h2>
      <p className="muted">Selamat datang kembali.</p>
      {error && <ErrorBox message={error} />}
      <label>
        Email atau username
        <input name="login" autoComplete="username" required />
      </label>
      <div className="auth-field">
        <div className="auth-field-head">
          <label htmlFor="login-password">Kata sandi</label>
          <Link to="/forgot-password">Lupa kata sandi?</Link>
        </div>
        <input id="login-password" name="password" type="password" autoComplete="current-password" required />
      </div>
      <button type="submit" className="btn btn-primary btn-block" disabled={busy}>
        {busy ? 'Memproses…' : 'Masuk'}
      </button>
      <p className="muted">
        Belum punya akun? <Link to="/register">Daftar</Link>
      </p>
    </form>
  )
}
