import type { CSSProperties } from 'react'
import type { DailyViews } from '../../api/types'
import { axisTop, formatDay, formatNumber, toBars, type Bar } from '../../lib/channelStats'

/**
 * Grafik batang "Penayangan per hari" (desain Studio.dc.html): sumbu Y
 * 0 / tengah / atas, garis putus-putus, dan batang tertinggi berwarna amber.
 *
 * Grafiknya digambar dengan div dan CSS saja (tinggi batang = persen),
 * tanpa library grafik. Untuk pembaca layar, angka yang sama tersedia di
 * tabel tersembunyi (.sr-only) di bawahnya.
 */
export function ViewsChart({ daily }: { daily: DailyViews[] }) {
  const { bars, daysPerBar } = toBars(daily)
  const peak = Math.max(0, ...bars.map((b) => b.views))
  const top = axisTop(peak)
  const unit = daysPerBar === 1 ? 'hari' : `${daysPerBar} hari`

  return (
    <section className="panel" aria-labelledby="views-chart-title">
      <div className="panel-head stats-chart-head">
        <h2 id="views-chart-title" className="panel-subtitle">
          Penayangan per {unit}
        </h2>
        <span className="muted small">Puncak: {formatNumber(peak)} tayangan</span>
      </div>

      <div className="chart-scroll" aria-hidden="true">
        <div className="chart">
          <div className="chart-axis">
            <span>{formatNumber(top)}</span>
            <span>{formatNumber(top / 2)}</span>
            <span>0</span>
          </div>
          <div className="chart-plot">
            <div className="chart-grid" style={{ top: 0 }} />
            <div className="chart-grid" style={{ top: '50%' }} />
            {peak === 0 && <p className="chart-empty muted small">Belum ada tayangan di periode ini.</p>}
            <div className={bars.length > 14 ? 'chart-bars is-dense' : 'chart-bars'}>
              {bars.map((b) => (
                <div
                  key={b.from}
                  className={peak > 0 && b.views === peak ? 'chart-bar is-peak' : 'chart-bar'}
                  style={{ '--h': `${(b.views / top) * 100}%` } as CSSProperties}
                  title={`${barLabel(b)}: ${formatNumber(b.views)} tayangan`}
                />
              ))}
            </div>
          </div>
          {bars.length > 0 && (
            <div className="chart-dates muted small">
              <span>{formatDay(bars[0].from)}</span>
              <span>{formatDay(bars[bars.length - 1].to)}</span>
            </div>
          )}
        </div>
      </div>

      <table className="sr-only">
        <caption>Penayangan per {unit}</caption>
        <thead>
          <tr>
            <th scope="col">Tanggal</th>
            <th scope="col">Tayangan</th>
          </tr>
        </thead>
        <tbody>
          {bars.map((b) => (
            <tr key={b.from}>
              <td>{barLabel(b)}</td>
              <td>{formatNumber(b.views)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted small">Tayangan per hari mulai dicatat sejak dasbor ini aktif. Tanggal memakai WIB.</p>
    </section>
  )
}

function barLabel(b: Bar): string {
  return b.from === b.to ? formatDay(b.from) : `${formatDay(b.from)} – ${formatDay(b.to)}`
}
