import { Link, Outlet } from 'react-router'
import { LogoMark, Wordmark } from './Logo'
import { ThemeToggle } from './ThemeToggle'

/**
 * Kerangka halaman Masuk dan Daftar, mengikuti desain "Masuk dan daftar":
 * panel brand di kiri, formulir di kanan. Tidak memakai header biasa.
 */
export function AuthLayout() {
  return (
    <div className="auth">
      <section className="auth-brand">
        <Link to="/" className="brand" aria-label="Kembali ke beranda Ice-Stream">
          <LogoMark size={44} />
          <Wordmark size={26} />
        </Link>
        <div className="auth-pitch">
          <h1>Nonton bareng, komentar melintas di layar.</h1>
          <p className="muted">Buat akun untuk menyukai video, melanjutkan tontonan, dan nanti mengirim danmaku.</p>
          <ul className="auth-points">
            <li>
              <span className="dot dot-mint" />
              Danmaku real-time di setiap video
            </li>
            <li>
              <span className="dot dot-amber" />
              Streaming adaptif 360p dan 720p
            </li>
            <li>
              <span className="dot dot-pink" />
              Lanjutkan menonton dari posisi terakhir
            </li>
          </ul>
        </div>
        <p className="faint small">© 2026 Ice-Stream</p>
      </section>
      <section className="auth-main">
        <div className="auth-toolbar">
          <ThemeToggle />
        </div>
        <Outlet />
      </section>
    </div>
  )
}
