import type { Danmaku } from '../../api/types'
import { formatClock } from '../../lib/danmaku'

const dayMonth = new Intl.DateTimeFormat('id-ID', { day: '2-digit', month: '2-digit' })

interface Props {
  items: Danmaku[]
  /** Dipanggil saat satu baris diklik: lompat ke detik danmaku itu. */
  onSeek: (ms: number) => void
}

/**
 * Kartu "Daftar danmaku" di kolom kanan (desain Watch.dc.html), terbaru
 * dikirim lebih dulu. Hanya berisi potongan 6 menit yang sudah dimuat, jadi
 * judulnya menulis "dimuat", bukan total.
 */
export function DanmakuList({ items, onSeek }: Props) {
  const sorted = [...items].sort((a, b) => b.created_at.localeCompare(a.created_at) || Number(b.id) - Number(a.id))
  return (
    <section className="dm-list" aria-labelledby="dm-list-title">
      <div className="dm-list-head">
        <h2 id="dm-list-title" className="side-title">
          Daftar danmaku
        </h2>
        <span className="muted small">{items.length} dimuat</span>
      </div>
      {sorted.length === 0 ? (
        <p className="muted small dm-list-empty">Belum ada danmaku di bagian ini. Jadilah yang pertama!</p>
      ) : (
        <ul className="dm-list-body">
          {sorted.map((d) => (
            <li key={d.id}>
              <button type="button" onClick={() => onSeek(d.position_ms)} title={`Lompat ke ${formatClock(d.position_ms)}`}>
                <span className="mono muted">{formatClock(d.position_ms)}</span>
                <span className="dm-list-text">{d.body}</span>
                <span className="faint">{dayMonth.format(new Date(d.created_at))}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
