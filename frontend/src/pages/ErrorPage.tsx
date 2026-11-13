import { isRouteErrorResponse, Link, useRouteError } from 'react-router'

/** Ditampilkan jika sebuah halaman melempar error saat render. */
export function ErrorPage() {
  const error = useRouteError()
  const message = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error
      ? error.message
      : 'Terjadi kesalahan.'
  return (
    <main className="page center">
      <h1 className="page-title">Ups, ada yang rusak</h1>
      <p className="muted">{message}</p>
      <Link to="/" className="btn">
        Kembali ke beranda
      </Link>
    </main>
  )
}
