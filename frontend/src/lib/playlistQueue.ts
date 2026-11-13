import type { PlaylistEntry } from '../api/types'

/** Posisi video yang sedang diputar di antrean playlist. */
export interface QueuePosition {
  /** Indeks video sekarang di entries, -1 jika (belum) ditemukan. */
  index: number
  /** Video berikutnya, atau null jika video sekarang yang terakhir (atau belum ditemukan). */
  next: PlaylistEntry | null
  /**
   * true jika halaman berikutnya perlu dimuat dulu: video sekarang belum
   * ditemukan, atau video sekarang adalah yang terakhir dari yang sudah dimuat.
   */
  needMore: boolean
}

/**
 * Menentukan posisi videoId di antrean. Isi playlist dimuat per halaman,
 * jadi "berikutnya" baru pasti setelah halaman yang memuat video
 * berikutnya ikut dimuat (hasMore = masih ada halaman lain di server).
 */
export function queuePosition(entries: PlaylistEntry[], videoId: string, hasMore: boolean): QueuePosition {
  const index = entries.findIndex((e) => e.video.id === videoId)
  if (index === -1) return { index, next: null, needMore: hasMore }
  const next = entries[index + 1] ?? null
  return { index, next, needMore: next === null && hasMore }
}

/** Alamat halaman tonton untuk satu video di dalam playlist. */
export function watchInListPath(videoId: string, playlistId: string): string {
  return `/watch/${encodeURIComponent(videoId)}?list=${encodeURIComponent(playlistId)}`
}
