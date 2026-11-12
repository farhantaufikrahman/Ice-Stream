// Angka di tab pencarian (langkah 27, desain Search.dc.html: "Video 1.170",
// "Sekitar 1.284 hasil untuk ...").

import type { SearchCounts, TabCount } from '../api/types'

export type CountTab = 'semua' | 'video' | 'kreator' | 'live' | 'playlist'

const number = new Intl.NumberFormat('id-ID')

/** Jumlah satu tab. Tab Semua = jumlah keempat tab. null jika belum ada atau gagal dihitung. */
export function tabCount(counts: SearchCounts | null, tab: CountTab): TabCount | null {
  if (!counts) return null
  switch (tab) {
    case 'video':
      return counts.video
    case 'kreator':
      return counts.channel
    case 'live':
      return counts.live
    case 'playlist':
      return counts.playlist
    case 'semua': {
      const parts = [counts.video, counts.channel, counts.live, counts.playlist]
      // Satu tab gagal: jumlah total tidak bisa dipercaya, jadi tidak ditampilkan.
      if (parts.some((p) => p === null)) return null
      return {
        count: parts.reduce((sum, p) => sum + (p?.count ?? 0), 0),
        capped: parts.some((p) => p?.capped),
      }
    }
  }
}

/** "1.284", atau "1.000+" jika hasilnya lebih banyak dari yang dihitung server. */
export function formatTabCount(c: TabCount): string {
  return number.format(c.count) + (c.capped ? '+' : '')
}

/** Kalimat di atas hasil: "Sekitar 1.284 hasil" / "Lebih dari 1.000 hasil" / "Tidak ada hasil". */
export function resultSummary(c: TabCount): string {
  if (c.capped) return `Lebih dari ${number.format(c.count)} hasil`
  if (c.count === 0) return 'Tidak ada hasil'
  return `Sekitar ${number.format(c.count)} hasil`
}
