// Aturan dan hitungan untuk danmaku (komentar berjalan di atas video).
// Batasnya sama dengan DanmakuService (backend/internal/danmaku/service.go).

import type { Danmaku } from '../api/types'

export const MAX_DANMAKU_CHARS = 100
export const MAX_COMMENT_CHARS = 1000

/** Server membatasi satu ListDanmaku maksimal 6 menit (maxWindowMs). */
export const DANMAKU_WINDOW_MS = 6 * 60 * 1000

/** Lama danmaku berjalan dari kanan ke kiri. Bilibili memakai sekitar 8 detik. */
export const SCROLL_DURATION_MS = 8000
/** Lama danmaku TOP/BOTTOM diam di tengah. */
export const FIXED_DURATION_MS = 4000

/** Warna pilihan di desain: Putih, Mint, Cone (amber), Stroberi. */
export const DANMAKU_COLORS = [
  { name: 'Putih', value: '#FFFFFF' },
  { name: 'Mint', value: '#4FE3C1' },
  { name: 'Cone', value: '#F5B451' },
  { name: 'Stroberi', value: '#FF6B8B' },
] as const

/** 252000 -> "04:12", 3725000 -> "1:02:05". */
export function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = String(Math.floor((total % 3600) / 60)).padStart(2, '0')
  const s = String(total % 60).padStart(2, '0')
  return h > 0 ? `${h}:${m}:${s}` : `${m}:${s}`
}

/** Nomor potongan 6 menit untuk posisi ms: 0 = 00:00-06:00, 1 = 06:00-12:00, dst. */
export function windowIndex(ms: number): number {
  return Math.max(0, Math.floor(ms / DANMAKU_WINDOW_MS))
}

/**
 * Perkiraan lebar teks dalam piksel, tanpa mengukur DOM. Huruf Latin kira-kira
 * 0,6 kali ukuran font; huruf CJK dan emoji hampir selebar ukuran font.
 */
export function estimateTextWidth(text: string, fontSize: number): number {
  let units = 0
  for (const ch of text) {
    units += (ch.codePointAt(0) ?? 0) > 0x2e80 ? 1 : 0.6
  }
  return Math.ceil(units * fontSize)
}

/** Danmaku terakhir di satu jalur (lane) danmaku berjalan. */
export interface LaneItem {
  /** Posisi video (ms) saat danmaku ini mulai masuk dari kanan. */
  start: number
  /** Lebar teksnya (px). */
  width: number
}

/**
 * Apakah danmaku baru (lebar `width`) boleh masuk jalur ini sekarang tanpa
 * menabrak danmaku sebelumnya. Semua danmaku butuh waktu sama (`duration`)
 * untuk menyeberang layar selebar `screen`, jadi teks yang lebih panjang
 * bergerak lebih cepat. Dua syarat:
 *   1. ekor danmaku sebelumnya sudah masuk layar, ditambah jarak `gap`;
 *   2. danmaku baru tidak menyusul danmaku sebelumnya sebelum keluar di kiri.
 */
export function laneFits(prev: LaneItem | null, now: number, width: number, screen: number, duration: number, gap = 24): boolean {
  if (!prev) return true
  const prevSpeed = (screen + prev.width) / duration
  const entered = now - prev.start >= (prev.width + gap) / prevSpeed
  const speed = (screen + width) / duration
  const noCatchUp = now + screen / speed >= prev.start + duration
  return entered && noCatchUp
}

/** Menyisipkan danmaku ke daftar yang urut posisi, tanpa duplikat id. */
export function mergeDanmaku(list: readonly Danmaku[], incoming: readonly Danmaku[]): Danmaku[] {
  const seen = new Set(list.map((d) => d.id))
  const fresh = incoming.filter((d) => !seen.has(d.id) && seen.add(d.id))
  if (fresh.length === 0) return list as Danmaku[]
  return [...list, ...fresh].sort((a, b) => a.position_ms - b.position_ms || Number(a.id) - Number(b.id))
}

/** Indeks pertama di list (urut posisi) dengan position_ms > ms (binary search). */
export function firstAfter(list: readonly Danmaku[], ms: number): number {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >>> 1
    if (list[mid].position_ms <= ms) lo = mid + 1
    else hi = mid
  }
  return lo
}
