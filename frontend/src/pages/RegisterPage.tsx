import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate } from 'react-router'
import { errorMessage } from '../api/client'
import { useAuth } from '../auth/useAuth'
import { ErrorBox } from '../components/ErrorBox'

// Aturan isian (panjang username, password, dan lainnya) diperiksa oleh
// AuthService. Pesan errornya ditampilkan apa adanya, jadi aturan tidak
// perlu ditulis dua kali di frontend.
export function RegisterPage() {
  const { status, register } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (status === 'authenticated' && !busy) {
    return <Navigate to="/" replace />
  }

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setBusy(true)
    setError(null)
    try {
      await register({
        username: String(form.get('username')).trim(),
        email: String(form.get('email')).trim(),
        password: String(form.get('password')),
        display_name: String(form.get('display_name')).trim(),
      })
      navigate('/', { replace: true })
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <form className="auth-card" onSubmit={onSubmit}>
      <h2>Daftar</h2>
      <p className="muted">Gratis, kurang dari satu menit.</p>
      {error && <ErrorBox message={error} />}
      <label>
        Username
        <input name="username" autoComplete="username" required />
      </label>
      <label>
        Nama tampilan
        <input name="display_name" autoComplete="nickname" />
      </label>
      <label>
        Email
        <input name="email" type="email" autoComplete="email" required />
      </label>
      <label>
        Kata sandi
        <input name="password" type="password" autoComplete="new-password" required />
      </label>
      <button type="submit" className="btn btn-amber btn-block" disabled={busy}>
        {busy ? 'Memproses…' : 'Buat akun'}
      </button>
      <p className="muted">
        Sudah punya akun? <Link to="/login">Masuk</Link>
      </p>
    </form>
  )
}
