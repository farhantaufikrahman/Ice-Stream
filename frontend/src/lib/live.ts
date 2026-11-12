// Hitungan kecil untuk halaman siaran langsung (langkah 25b dan 25c).

import type { Danmaku, LiveChatMessage, LiveStream } from '../api/types'
import { formatClock } from './danmaku'

/** Lama siaran sejak startedAt, misalnya "04:12" atau "1:12:40". */
export function liveElapsed(startedAt: string, now: number = Date.now()): string {
  return formatClock(now - new Date(startedAt).getTime())
}

/** Jam mulai siaran, misalnya "07.10" (zona waktu browser). */
export function liveStartClock(startedAt: string): string {
  return new Date(startedAt).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' })
}

/**
 * Pesan chat sebagai danmaku, supaya bisa melayang di atas video memakai
 * DanmakuLayer yang sama dengan video biasa. Awalan "chat-" mencegah
 * bentrok id dengan danmaku video.
 */
export function chatAsDanmaku(m: LiveChatMessage): Danmaku {
  return { id: `chat-${m.id}`, video_id: '', position_ms: 0, body: m.body, color: m.color, mode: 'SCROLL', created_at: m.created_at }
}

/** Menambahkan pesan ke daftar chat tanpa dobel, dan membatasi panjangnya. */
export function appendChat(list: readonly LiveChatMessage[], incoming: readonly LiveChatMessage[], max = 200): LiveChatMessage[] {
  const seen = new Set(list.map((m) => m.id))
  const fresh = incoming.filter((m) => !seen.has(m.id) && seen.add(m.id))
  if (fresh.length === 0) return list as LiveChatMessage[]
  const merged = [...list, ...fresh].sort((a, b) => Number(a.id) - Number(b.id))
  return merged.length > max ? merged.slice(merged.length - max) : merged
}

/** Ringkasan statistik kirim WebRTC pada satu waktu. */
export interface SendSample {
  /** Waktu pengukuran (ms). */
  timestamp: number
  /** Total byte terkirim (video + suara). */
  bytes: number
  width: number
  height: number
  fps: number
  /** Alasan kualitas diturunkan browser: "none", "bandwidth", "cpu", ... */
  limitation: string
}

/** Bagian RTCStats yang dibaca. https://www.w3.org/TR/webrtc-stats/#outboundrtpstats-dict* */
interface OutboundStats {
  type: string
  kind?: string
  timestamp: number
  bytesSent?: number
  frameWidth?: number
  frameHeight?: number
  framesPerSecond?: number
  qualityLimitationReason?: string
}

/** Menjumlahkan statistik outbound-rtp dari pc.getStats(). */
export function sampleStats(report: Iterable<OutboundStats>): SendSample {
  const out: SendSample = { timestamp: 0, bytes: 0, width: 0, height: 0, fps: 0, limitation: 'none' }
  for (const s of report) {
    if (s.type !== 'outbound-rtp') continue
    out.timestamp = Math.max(out.timestamp, s.timestamp)
    out.bytes += s.bytesSent ?? 0
    if (s.kind === 'video') {
      out.width = s.frameWidth ?? 0
      out.height = s.frameHeight ?? 0
      out.fps = Math.round(s.framesPerSecond ?? 0)
      out.limitation = s.qualityLimitationReason ?? 'none'
    }
  }
  return out
}

/** Bitrate kirim (bit per detik) di antara dua pengukuran. */
export function bitrate(prev: SendSample | null, cur: SendSample): number {
  if (!prev || cur.timestamp <= prev.timestamp || cur.bytes < prev.bytes) return 0
  return ((cur.bytes - prev.bytes) * 8 * 1000) / (cur.timestamp - prev.timestamp)
}

/** 2_800_000 -> "2,8 Mbps", 640_000 -> "640 kbps". */
export function formatBitrate(bps: number): string {
  if (bps >= 1_000_000) {
    return `${(bps / 1_000_000).toLocaleString('id-ID', { maximumFractionDigits: 1 })} Mbps`
  }
  return `${Math.round(bps / 1000)} kbps`
}

/** Teks status koneksi WebRTC. */
export function connectionLabel(state: RTCPeerConnectionState | 'idle'): string {
  switch (state) {
    case 'connected':
      return 'Tersambung'
    case 'new':
    case 'connecting':
      return 'Menyambung…'
    case 'disconnected':
      return 'Terputus, mencoba lagi…'
    case 'failed':
      return 'Gagal tersambung'
    case 'closed':
      return 'Ditutup'
    default:
      return '—'
  }
}

/** Alasan browser menurunkan kualitas video, dalam bahasa sehari-hari. */
export function limitationLabel(reason: string): string | null {
  switch (reason) {
    case 'bandwidth':
      return 'Kualitas diturunkan karena koneksi lambat'
    case 'cpu':
      return 'Kualitas diturunkan karena perangkat sibuk'
    default:
      return null
  }
}

/**
 * Warna latar kartu, diambil dari desain (Home.dc.html, "Sedang live").
 * MediaMTX tidak membuat gambar thumbnail siaran, jadi kartu memakai warna
 * polos + logo, dipilih tetap per kreator supaya tidak berganti-ganti.
 */
const TONES = ['#1F3B4D', '#3B2F5E', '#5A3E2B', '#24405E', '#2E4A3F']

export function liveTone(ownerId: string): string {
  let n = 0
  for (const ch of ownerId) n = (n * 31 + ch.charCodeAt(0)) % 1_000_003
  return TONES[n % TONES.length]
}

export function liveOwnerName(s: LiveStream): string {
  return s.owner?.display_name || s.owner?.username || 'Kreator'
}
