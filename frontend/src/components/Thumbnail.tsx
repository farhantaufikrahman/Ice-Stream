import { useState } from 'react'

/** Gambar thumbnail 16:9. Jika kosong atau gagal dimuat, tampil kotak polos. */
export function Thumbnail({ url, title }: { url?: string; title: string }) {
  const [broken, setBroken] = useState(false)
  if (!url || broken) {
    return <div className="thumb thumb-empty" role="img" aria-label={title} />
  }
  return <img className="thumb" src={url} alt={title} loading="lazy" onError={() => setBroken(true)} />
}
