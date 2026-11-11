import { describe, expect, it } from 'vitest'
import type { PlaylistEntry, Video } from '../api/types'
import { queuePosition, watchInListPath } from './playlistQueue'

function entry(id: string): PlaylistEntry {
  return { video: { id } as Video, added_at: '2026-11-11T08:00:00Z' }
}

describe('queuePosition', () => {
  const entries = [entry('1'), entry('2'), entry('3')]

  it('video di tengah: berikutnya langsung diketahui', () => {
    expect(queuePosition(entries, '2', true)).toEqual({ index: 1, next: entries[2], needMore: false })
  })

  it('video terakhir yang dimuat: muat halaman berikutnya jika masih ada', () => {
    expect(queuePosition(entries, '3', true)).toEqual({ index: 2, next: null, needMore: true })
    expect(queuePosition(entries, '3', false)).toEqual({ index: 2, next: null, needMore: false })
  })

  it('video belum ditemukan: terus muat selama masih ada halaman', () => {
    expect(queuePosition(entries, '9', true)).toEqual({ index: -1, next: null, needMore: true })
    expect(queuePosition(entries, '9', false)).toEqual({ index: -1, next: null, needMore: false })
  })
})

it('watchInListPath', () => {
  expect(watchInListPath('42', '5')).toBe('/watch/42?list=5')
})
