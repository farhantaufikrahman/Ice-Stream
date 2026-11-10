import { describe, expect, it } from 'vitest'
import { formatDuration, safeNextPath, timeAgo } from './format'

describe('formatDuration', () => {
  it.each([
    [0, '0:00'],
    [5_000, '0:05'],
    [75_000, '1:15'],
    [3_723_000, '1:02:03'],
  ])('%d ms -> %s', (ms, want) => {
    expect(formatDuration(ms)).toBe(want)
  })
})

describe('timeAgo', () => {
  const now = new Date('2026-11-10T12:00:00Z')
  it('baru saja', () => {
    expect(timeAgo('2026-11-10T11:59:30Z', now)).toBe('baru saja')
  })
  it('jam dan hari', () => {
    expect(timeAgo('2026-11-10T09:00:00Z', now)).toBe('3 jam yang lalu')
    expect(timeAgo('2026-11-07T12:00:00Z', now)).toBe('3 hari yang lalu')
  })
})

describe('safeNextPath', () => {
  it.each([
    ['/watch/1', '/watch/1'],
    [null, '/'],
    ['https://jahat.example', '/'],
    ['//jahat.example', '/'],
    ['/\\jahat.example', '/'],
  ])('%s -> %s', (next, want) => {
    expect(safeNextPath(next)).toBe(want)
  })
})
