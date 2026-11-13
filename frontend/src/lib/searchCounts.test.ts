import { describe, expect, it } from 'vitest'
import type { SearchCounts } from '../api/types'
import { formatTabCount, resultSummary, tabCount } from './searchCounts'

const counts: SearchCounts = {
  video: { count: 1170, capped: false },
  channel: { count: 42, capped: false },
  live: { count: 3, capped: false },
  playlist: { count: 69, capped: false },
}

describe('searchCounts', () => {
  it('tab Semua menjumlahkan semua tab (contoh desain)', () => {
    const all = tabCount(counts, 'semua')
    expect(all).toEqual({ count: 1284, capped: false })
    expect(formatTabCount(all!)).toBe('1.284')
    expect(resultSummary(all!)).toBe('Sekitar 1.284 hasil')
  })

  it('hasil lebih dari batas hitung server', () => {
    const many = { ...counts, video: { count: 1000, capped: true } }
    expect(formatTabCount(tabCount(many, 'video')!)).toBe('1.000+')
    expect(resultSummary(tabCount(many, 'semua')!)).toBe('Lebih dari 1.114 hasil')
  })

  it('tab yang gagal dihitung tidak ditampilkan', () => {
    const partial = { ...counts, live: null }
    expect(tabCount(partial, 'live')).toBeNull()
    expect(tabCount(partial, 'semua')).toBeNull()
    expect(tabCount(partial, 'kreator')).toEqual({ count: 42, capped: false })
    expect(tabCount(null, 'video')).toBeNull()
    expect(resultSummary({ count: 0, capped: false })).toBe('Tidak ada hasil')
  })
})
