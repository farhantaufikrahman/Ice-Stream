import { describe, expect, it } from 'vitest'
import type { LiveChatMessage } from '../api/types'
import { appendChat, bitrate, chatAsDanmaku, connectionLabel, formatBitrate, liveElapsed, liveTone, sampleStats } from './live'

function msg(id: string, body = 'halo'): LiveChatMessage {
  return { id, stream_id: '3', user: null, body, color: '#4FE3C1', created_at: '2026-11-12T07:00:00Z' }
}

describe('live', () => {
  it('lama siaran', () => {
    const start = '2026-11-12T07:00:00Z'
    expect(liveElapsed(start, Date.parse('2026-11-12T07:04:12Z'))).toBe('04:12')
    expect(liveElapsed(start, Date.parse('2026-11-12T08:12:40Z'))).toBe('1:12:40')
    // Jam browser sedikit di belakang server: tidak negatif.
    expect(liveElapsed(start, Date.parse('2026-11-12T06:59:58Z'))).toBe('00:00')
  })

  it('chat menjadi danmaku melayang', () => {
    expect(chatAsDanmaku(msg('15', 'mantap'))).toMatchObject({ id: 'chat-15', body: 'mantap', color: '#4FE3C1', mode: 'SCROLL' })
  })

  it('menggabungkan chat tanpa dobel, urut, dan dibatasi', () => {
    const list = appendChat([msg('2'), msg('3')], [msg('3'), msg('1'), msg('10')])
    expect(list.map((m) => m.id)).toEqual(['1', '2', '3', '10'])
    const same = [msg('1')]
    expect(appendChat(same, [msg('1')])).toBe(same)
    expect(appendChat([msg('1'), msg('2')], [msg('3')], 2).map((m) => m.id)).toEqual(['2', '3'])
  })

  it('statistik kirim WebRTC', () => {
    const a = sampleStats([
      { type: 'outbound-rtp', kind: 'video', timestamp: 1000, bytesSent: 100_000, frameWidth: 1280, frameHeight: 720, framesPerSecond: 29.6, qualityLimitationReason: 'none' },
      { type: 'outbound-rtp', kind: 'audio', timestamp: 1000, bytesSent: 10_000 },
      { type: 'candidate-pair', timestamp: 1000 },
    ])
    expect(a).toEqual({ timestamp: 1000, bytes: 110_000, width: 1280, height: 720, fps: 30, limitation: 'none' })
    const b = { ...a, timestamp: 3000, bytes: 810_000 }
    // 700.000 byte dalam 2 detik = 2,8 Mbps.
    expect(bitrate(a, b)).toBe(2_800_000)
    expect(bitrate(null, b)).toBe(0)
    expect(formatBitrate(2_800_000)).toBe('2,8 Mbps')
    expect(formatBitrate(640_000)).toBe('640 kbps')
  })

  it('label koneksi dan warna kartu tetap per kreator', () => {
    expect(connectionLabel('connected')).toBe('Tersambung')
    expect(connectionLabel('idle')).toBe('—')
    expect(liveTone('7')).toBe(liveTone('7'))
    expect(liveTone('7')).toMatch(/^#[0-9A-F]{6}$/)
  })
})
