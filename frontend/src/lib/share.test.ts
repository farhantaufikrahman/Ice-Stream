import { describe, expect, it } from 'vitest'
import { parseStartParam, shareUrl } from './share'

describe('shareUrl', () => {
  it('tanpa waktu mulai', () => {
    expect(shareUrl('http://ice-stream.localhost:4000', '42')).toBe('http://ice-stream.localhost:4000/watch/42')
    expect(shareUrl('http://x.test', '42', 0.9)).toBe('http://x.test/watch/42')
  })
  it('dengan waktu mulai dibulatkan ke bawah', () => {
    expect(shareUrl('http://x.test', '42', 83.7)).toBe('http://x.test/watch/42?t=83')
  })
})

describe('parseStartParam', () => {
  it('angka detik dan bentuk 1m23s', () => {
    expect(parseStartParam('83')).toBe(83)
    expect(parseStartParam('1m23s')).toBe(83)
    expect(parseStartParam('1h2m')).toBe(3720)
    expect(parseStartParam('45s')).toBe(45)
  })
  it('nilai rusak atau di luar batas menjadi 0', () => {
    for (const v of [null, '', '0', '-5', 'abc', '1.5', 'm', '90000', '1x']) {
      expect(parseStartParam(v)).toBe(0)
    }
  })
})
