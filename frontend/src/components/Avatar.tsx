import { useState } from 'react'

// Warna latar inisial dari desain (ungu, mint, biru muda, amber, stroberi,
// hijau limau). Dipilih dari nama, jadi user yang sama selalu berwarna sama.
const COLORS = ['#B9A3FF', '#4FE3C1', '#8FD8FF', '#F5B451', '#FF6B8B', '#C8E66B']

function colorFor(name: string): string {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return COLORS[h % COLORS.length]
}

/** "Budi Santoso" -> "BS", "tono" -> "TO". */
function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

interface Props {
  name: string
  url?: string
  size?: number
}

/** Foto profil, atau inisial berwarna jika belum ada / gagal dimuat. */
export function Avatar({ name, url, size = 36 }: Props) {
  const [broken, setBroken] = useState(false)
  const style = { width: size, height: size }
  if (url && !broken) {
    return <img className="avatar" src={url} alt="" style={style} onError={() => setBroken(true)} />
  }
  return (
    <span
      className="avatar avatar-fallback"
      style={{ ...style, fontSize: Math.round(size * 0.36), background: colorFor(name) }}
      aria-hidden="true"
    >
      {initials(name)}
    </span>
  )
}
