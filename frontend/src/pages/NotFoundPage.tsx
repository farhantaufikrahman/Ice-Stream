import { Link } from 'react-router'

export function NotFoundPage() {
  return (
    <div className="center">
      <h1 className="page-title">Halaman tidak ditemukan</h1>
      <Link to="/" className="btn">
        Kembali ke beranda
      </Link>
    </div>
  )
}
