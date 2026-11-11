// Fungsi format tampilan. Memakai Intl bawaan browser, jadi tidak perlu
// library seperti moment.js / dayjs.
// https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Intl

const compact = new Intl.NumberFormat('id-ID', { notation: 'compact', maximumFractionDigits: 1 })
const relative = new Intl.RelativeTimeFormat('id-ID', { numeric: 'auto' })

/** 75000 -> "1:15", 3723000 -> "1:02:03". */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

/** 1234 -> "1,2 rb", 2500000 -> "2,5 jt". */
export function formatCount(n: number): string {
  return compact.format(n)
}

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['week', 7 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
]

/** "3 hari yang lalu", "kemarin", "baru saja". */
export function timeAgo(iso: string, now: Date = new Date()): string {
  const seconds = Math.round((new Date(iso).getTime() - now.getTime()) / 1000)
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.trunc(seconds / size), unit)
    }
  }
  return 'baru saja'
}

/**
 * Hanya menerima path di situs ini ("/watch/1"), supaya ?next= tidak bisa
 * dipakai untuk mengalihkan user ke situs lain (open redirect).
 */
export function safeNextPath(next: string | null): string {
  if (next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/\\')) {
    return next
  }
  return '/'
}

const dateFormat = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })

/** "2026-10-18T..." -> "18 Okt 2026". */
export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso))
}

/** Angka di lencana lonceng: 7 -> "7", 100 -> "99+" (server berhenti menghitung di 100). */
export function badgeText(count: number): string {
  return count > 99 ? '99+' : String(count)
}
