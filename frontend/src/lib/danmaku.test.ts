import { describe, expect, it } from 'vitest'
import type { Danmaku } from '../api/types'
import { estimateTextWidth, firstAfter, formatClock, laneFits, mergeDanmaku, windowIndex } from './danmaku'

const dm = (id: string, position_ms: number): Danmaku => ({
  id,
  video_id: '1',
  position_ms,
  body: 'x',
  color: '#FFFFFF',
  mode: 'SCROLL',
  created_at: '2026-11-11T00:00:00Z',
})

describe('format dan potongan', () => {
  it('formatClock', () => {
    expect(formatClock(252_000)).toBe('04:12')
    expect(formatClock(3_725_000)).toBe('1:02:05')
    expect(formatClock(-5)).toBe('00:00')
  })

  it('windowIndex per 6 menit', () => {
    expect(windowIndex(0)).toBe(0)
    expect(windowIndex(359_999)).toBe(0)
    expect(windowIndex(360_000)).toBe(1)
  })

  it('estimateTextWidth: emoji/CJK lebih lebar dari huruf Latin', () => {
    expect(estimateTextWidth('abcde', 20)).toBe(60)
    expect(estimateTextWidth('弾幕', 20)).toBe(40)
  })
})

describe('laneFits', () => {
  const screen = 800
  const duration = 8000

  it('jalur kosong selalu boleh', () => {
    expect(laneFits(null, 0, 100, screen, duration)).toBe(true)
  })

  it('menunggu ekor danmaku sebelumnya masuk layar', () => {
    const prev = { start: 0, width: 200 }
    // kecepatan prev = 1000/8000 = 0,125 px/ms; masuk + jarak 24 px butuh 1792 ms
    expect(laneFits(prev, 1000, 200, screen, duration)).toBe(false)
    expect(laneFits(prev, 1800, 200, screen, duration)).toBe(true)
  })

  it('teks jauh lebih panjang (lebih cepat) tidak boleh menyusul', () => {
    const prev = { start: 0, width: 50 }
    expect(laneFits(prev, 1000, 1200, screen, duration)).toBe(false)
  })
})

describe('mergeDanmaku dan firstAfter', () => {
  it('urut posisi dan tanpa duplikat', () => {
    const list = mergeDanmaku([dm('1', 5000)], [dm('2', 1000), dm('1', 5000), dm('3', 9000)])
    expect(list.map((d) => d.id)).toEqual(['2', '1', '3'])
  })

  it('mengembalikan array yang sama jika tidak ada yang baru', () => {
    const list = [dm('1', 5000)]
    expect(mergeDanmaku(list, [dm('1', 5000)])).toBe(list)
  })

  it('firstAfter mencari indeks pertama setelah posisi', () => {
    const list = [dm('1', 1000), dm('2', 2000), dm('3', 2000), dm('4', 5000)]
    expect(firstAfter(list, 0)).toBe(0)
    expect(firstAfter(list, 2000)).toBe(3)
    expect(firstAfter(list, 9000)).toBe(4)
  })
})
