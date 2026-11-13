// Perhitungan tampilan dasbor Creator Studio (desain Studio.dc.html).
// Dipisah dari komponen supaya mudah dites tanpa React.
import type { DailyViews } from '../api/types'

const fullNumber = new Intl.NumberFormat('id-ID')
const shortDate = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'short' })
const longDate = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })

/** 1204311 -> "1.204.311" (angka besar di kartu, sama seperti desain). */
export function formatNumber(n: number): string {
  return fullNumber.format(n)
}

/**
 * "2026-11-05" -> Date 5 Nov 2026 pukul 00:00 waktu lokal. new Date("2026-11-05")
 * dibaca sebagai UTC, sehingga di zona waktu negatif tanggalnya mundur sehari.
 */
function parseDay(day: string): Date {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(y, m - 1, d)
}

/** "2026-11-05" -> "5 Nov". */
export function formatDay(day: string): string {
  return shortDate.format(parseDay(day))
}

/** Rentang periode, misalnya "5 Nov – 11 Nov 2026". */
export function formatPeriod(daily: DailyViews[]): string {
  if (daily.length === 0) return ''
  const first = daily[0].date
  const last = daily[daily.length - 1].date
  return `${formatDay(first)} – ${longDate.format(parseDay(last))}`
}

export type DeltaTone = 'up' | 'down' | 'flat'

export interface Delta {
  label: string
  tone: DeltaTone
}

/**
 * Perbandingan dengan periode lalu, seperti "+18% dari periode lalu" di
 * desain. Jika periode lalu 0, persentase tidak bisa dihitung (dibagi nol),
 * jadi labelnya berupa kalimat.
 */
export function compareWithPrevious(current: number, previous: number): Delta {
  if (previous === 0) {
    if (current === 0) return { label: 'Belum ada di kedua periode', tone: 'flat' }
    return { label: 'Naik dari 0 di periode lalu', tone: 'up' }
  }
  const pct = Math.round(((current - previous) / previous) * 100)
  if (pct === 0) return { label: 'Sama dengan periode lalu', tone: 'flat' }
  // U+2212 (tanda minus), bukan tanda hubung, sama seperti desain.
  const sign = pct > 0 ? '+' : '−'
  return { label: `${sign}${Math.abs(pct)}% dari periode lalu`, tone: pct > 0 ? 'up' : 'down' }
}

export interface Bar {
  /** Tanggal pertama dan terakhir yang dijumlahkan di batang ini. */
  from: string
  to: string
  views: number
}

/**
 * Mengelompokkan data harian menjadi batang grafik. 7 dan 28 hari: satu
 * batang per hari. 90 hari: satu batang per 3 hari (30 batang), supaya
 * batangnya tidak terlalu tipis (desain juga menampilkan 30 batang).
 */
export function toBars(daily: DailyViews[]): { bars: Bar[]; daysPerBar: number } {
  const daysPerBar = daily.length > 31 ? Math.ceil(daily.length / 30) : 1
  const bars: Bar[] = []
  for (let i = 0; i < daily.length; i += daysPerBar) {
    const group = daily.slice(i, i + daysPerBar)
    bars.push({
      from: group[0].date,
      to: group[group.length - 1].date,
      views: group.reduce((sum, d) => sum + d.views, 0),
    })
  }
  return { bars, daysPerBar }
}

/**
 * Nilai teratas sumbu Y: angka "bulat" terkecil yang >= max, dari deret
 * 2, 4, 6, 8, 10, 20, 40, ... Semuanya genap, jadi garis tengah (top / 2)
 * selalu bilangan bulat. Minimal 2 supaya grafik kosong tetap punya sumbu.
 */
export function axisTop(max: number): number {
  if (max <= 2) return 2
  // Mulai satu tingkat di bawah jumlah digit max, misalnya max 48210 ->
  // step 1000 (2000 ... 10000, belum cukup) -> step 10000 -> 60000.
  let step = max < 10 ? 1 : 10 ** (Math.floor(Math.log10(max)) - 1)
  for (;;) {
    for (const m of [2, 4, 6, 8, 10]) {
      if (m * step >= max) return m * step
    }
    step *= 10
  }
}
