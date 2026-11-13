import type { ChannelStats } from '../../api/types'
import { compareWithPrevious, formatNumber } from '../../lib/channelStats'

/**
 * Empat kartu angka dasbor (desain Studio.dc.html). Dipakai Dasbor dan
 * Analitik. Untuk satu video (stats.video_id terisi), "Pengikut baru" tetap
 * angka seluruh channel karena subscribe tidak terkait satu video.
 */
export function StatsKpis({ stats }: { stats: ChannelStats }) {
  const { current: c, previous: p } = stats
  const cards: { label: string; value: number | null; previous: number | null }[] = [
    { label: 'Tayangan', value: c.views, previous: p.views },
    { label: stats.video_id ? 'Pengikut baru (channel)' : 'Pengikut baru', value: c.new_subscribers, previous: p.new_subscribers },
    { label: 'Danmaku diterima', value: c.danmaku, previous: p.danmaku },
    { label: 'Komentar diterima', value: c.comments, previous: p.comments },
  ]
  return (
    <div className="kpi-grid">
      {cards.map((k) => {
        const delta = k.value !== null && k.previous !== null ? compareWithPrevious(k.value, k.previous) : null
        return (
          <div key={k.label} className="kpi">
            <span className="kpi-label">{k.label}</span>
            <span className="kpi-value">{k.value === null ? '—' : formatNumber(k.value)}</span>
            {delta ? (
              <span className={`kpi-delta is-${delta.tone}`}>{delta.label}</span>
            ) : (
              <span className="kpi-delta is-flat">Sedang tidak bisa dimuat</span>
            )}
          </div>
        )
      })}
    </div>
  )
}

