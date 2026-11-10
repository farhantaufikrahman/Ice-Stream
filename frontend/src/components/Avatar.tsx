import { useState } from 'react'

interface Props {
  name: string
  url?: string
  size?: number
}

/** Foto profil, atau huruf pertama nama jika belum ada / gagal dimuat. */
export function Avatar({ name, url, size = 36 }: Props) {
  const [broken, setBroken] = useState(false)
  const style = { width: size, height: size, fontSize: size * 0.45 }
  if (url && !broken) {
    return <img className="avatar" src={url} alt="" style={style} onError={() => setBroken(true)} />
  }
  return (
    <span className="avatar avatar-fallback" style={style} aria-hidden="true">
      {(name.trim()[0] ?? '?').toUpperCase()}
    </span>
  )
}
