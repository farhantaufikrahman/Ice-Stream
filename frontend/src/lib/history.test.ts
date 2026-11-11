import { describe, expect, it } from 'vitest'
import { badgeText } from './format'
import { watchedFraction } from './history'

describe('watchedFraction', () => {
  it('posisi dibagi durasi', () => {
    expect(watchedFraction(30_000, 120_000)).toBe(0.25)
  })
  it('0 jika durasi tidak diketahui atau belum ditonton', () => {
    expect(watchedFraction(30_000, undefined)).toBe(0)
    expect(watchedFraction(30_000, 0)).toBe(0)
    expect(watchedFraction(0, 120_000)).toBe(0)
  })
  it('tidak pernah lebih dari 1', () => {
    expect(watchedFraction(120_400, 120_000)).toBe(1)
  })
})

describe('badgeText', () => {
  it('angka apa adanya sampai 99, lalu "99+"', () => {
    expect(badgeText(7)).toBe('7')
    expect(badgeText(99)).toBe('99')
    expect(badgeText(100)).toBe('99+')
  })
})
