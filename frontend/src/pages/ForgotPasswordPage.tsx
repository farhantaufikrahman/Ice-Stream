import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { requestPasswordReset } from '../api/endpoints'
import { ErrorBox } from '../components/ErrorBox'

/**
 * Langkah 24c: "Lupa kata sandi?". Server selalu menjawab sukses (selama
 * format email benar), jadi halaman ini juga tidak pernah mengatakan
 * apakah email itu terdaftar atau tidak.
 */
export function ForgotPasswordPage() {
  const [sentTo, setSentTo] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const email = String(new FormData(e.currentTarget).get('email')).trim()
    setBusy(true)
    setError(null)
    try {
      await requestPasswordReset(email)
      setSentTo(email)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  if (sentTo) {
    return (
      <div className="auth-card">
        <h2>Cek email kamu</h2>
        <p className="muted">
          Jika <strong className="auth-email">{sentTo}</strong> terdaftar di Ice-Stream, kami sudah mengirim tautan untuk
          membuat kata sandi baru. Tautannya hanya berlaku sebentar dan hanya bisa dipakai sekali.
        </p>
        <p className="muted small">Tidak ada email? Periksa folder spam, atau tunggu satu menit lalu minta lagi.</p>
        <button type="button" className="btn btn-block" onClick={() => setSentTo(null)}>
          Kirim ke email lain
        </button>
        <p className="muted">
          Sudah ingat? <Link to="/login">Masuk</Link>
        </p>
      </div>
    )
  }

  return (
    <form className="auth-card" onSubmit={onSubmit}>
      <h2>Lupa kata sandi</h2>
      <p className="muted">Masukkan email akunmu. Kami kirim tautan untuk membuat kata sandi baru.</p>
      {error && <ErrorBox message={error} />}
      <label>
        Email
        <input name="email" type="email" autoComplete="email" required autoFocus />
      </label>
      <button type="submit" className="btn btn-primary btn-block" disabled={busy}>
        {busy ? 'Mengirim…' : 'Kirim tautan'}
      </button>
      <p className="muted">
        Sudah ingat? <Link to="/login">Masuk</Link>
      </p>
    </form>
  )
}
