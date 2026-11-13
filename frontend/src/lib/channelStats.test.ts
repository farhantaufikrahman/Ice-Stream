import { describe, expect, it } from 'vitest'
import { axisTop, compareWithPrevious, formatDay, formatNumber, formatPeriod, toBars } from './channelStats'

const days = (n: number, views = (i: number) => i) =>
  Array.from({ length: n }, (_, i) => {
    const d = new Date(2026, 7, 1 + i) // mulai 1 Agustus 2026
    const date = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    return { date, views: views(i) }
  })

describe('compareWithPrevious', () => {
  it('naik dan turun dalam persen', () => {
    expect(compareWithPrevious(118, 100)).toEqual({ label: '+18% dari periode lalu', tone: 'up' })
    expect(compareWithPrevious(97, 100)).toEqual({ label: '−3% dari periode lalu', tone: 'down' })
    expect(compareWithPrevious(100, 100)).toEqual({ label: 'Sama dengan periode lalu', tone: 'flat' })
  })
  it('periode lalu nol: tanpa persen', () => {
    expect(compareWithPrevious(5, 0)).toEqual({ label: 'Naik dari 0 di periode lalu', tone: 'up' })
    expect(compareWithPrevious(0, 0).tone).toBe('flat')
  })
})

describe('toBars', () => {
  it('7 dan 28 hari: satu batang per hari', () => {
    const { bars, daysPerBar } = toBars(days(28))
    expect(daysPerBar).toBe(1)
    expect(bars).toHaveLength(28)
    expect(bars[0]).toEqual({ from: '2026-08-01', to: '2026-08-01', views: 0 })
  })
  it('90 hari: 30 batang, masing-masing 3 hari', () => {
    const { bars, daysPerBar } = toBars(days(90, () => 1))
    expect(daysPerBar).toBe(3)
    expect(bars).toHaveLength(30)
    expect(bars[0]).toEqual({ from: '2026-08-01', to: '2026-08-03', views: 3 })
    expect(bars[29].to).toBe('2026-10-29')
  })
})

describe('axisTop', () => {
  it.each([
    [0, 2],
    [1, 2],
    [3, 4],
    [7, 8],
    [9, 10],
    [10, 10],
    [11, 20],
    [999, 1000],
    [48210, 60000],
    [100000, 100000],
  ])('max %i -> %i', (max, top) => {
    expect(axisTop(max)).toBe(top)
  })
})

describe('format', () => {
  it('angka dan tanggal Indonesia', () => {
    expect(formatNumber(1204311)).toBe('1.204.311')
    expect(formatDay('2026-11-05')).toBe('5 Nov')
    expect(formatPeriod([{ date: '2026-11-05', views: 0 }, { date: '2026-11-11', views: 0 }])).toBe('5 Nov – 11 Nov 2026')
  })
})
