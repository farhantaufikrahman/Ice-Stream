import { describe, expect, it } from 'vitest'
import { channelSortFromParam, durationFromParam, searchSortFromParam, withParam } from './discover'

describe('nilai dari URL', () => {
  it('mengenal nilai yang diterima backend', () => {
    expect(searchSortFromParam('danmaku')).toBe('danmaku')
    expect(durationFromParam('medium')).toBe('medium')
    expect(channelSortFromParam('popular')).toBe('popular')
  })
  it('nilai kosong atau salah ketik menjadi bawaan', () => {
    expect(searchSortFromParam(null)).toBe('relevance')
    expect(searchSortFromParam('VIEWS')).toBe('relevance')
    expect(durationFromParam('10')).toBe('')
    expect(channelSortFromParam('views')).toBe('newest')
  })
})

describe('withParam', () => {
  it('mengganti satu parameter dan membiarkan yang lain', () => {
    const p = new URLSearchParams('q=es+krim&tab=video')
    expect(withParam(p, 'sort', 'views', 'relevance').toString()).toBe('q=es+krim&tab=video&sort=views')
    expect(p.toString()).toBe('q=es+krim&tab=video')
  })
  it('nilai bawaan dihapus dari URL', () => {
    const p = new URLSearchParams('q=es&sort=views&duration=short')
    expect(withParam(p, 'sort', 'relevance', 'relevance').toString()).toBe('q=es&duration=short')
    expect(withParam(p, 'duration', '').toString()).toBe('q=es&sort=views')
  })
})
