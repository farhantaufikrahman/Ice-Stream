import type { Video } from '../api/types'

// Langkah 24a: pilihan urutan dan filter yang dikirim ke backend.
// Nilai di sini sama persis dengan yang diterima gateway
// (backend/internal/httpserver/discover.go). Nilai lain dijawab 400.

export type SearchSort = 'relevance' | 'newest' | 'views' | 'danmaku'
export type DurationFilter = '' | 'short' | 'medium' | 'long'
export type ChannelSort = 'newest' | 'popular'

/** Tombol "Urutkan" di halaman pencarian (desain Search.dc.html). */
export const SEARCH_SORTS: { value: SearchSort; label: string }[] = [
  { value: 'relevance', label: 'Paling relevan' },
  { value: 'newest', label: 'Terbaru' },
  { value: 'views', label: 'Paling banyak ditonton' },
  { value: 'danmaku', label: 'Danmaku terbanyak' },
]

/** Tombol "Durasi". '' (semua durasi) tidak punya tombol: klik lagi tombol aktif untuk melepasnya. */
export const DURATIONS: { value: Exclude<DurationFilter, ''>; label: string }[] = [
  { value: 'short', label: '< 10 menit' },
  { value: 'medium', label: '10–30 menit' },
  { value: 'long', label: '> 30 menit' },
]

/** Tombol urutan di tab Video halaman kanal (desain Channel.dc.html). */
export const CHANNEL_SORTS: { value: ChannelSort; label: string }[] = [
  { value: 'newest', label: 'Terbaru' },
  { value: 'popular', label: 'Terpopuler' },
]

// Nilai dari URL bisa diketik siapa saja. Nilai yang tidak dikenal
// diperlakukan seperti bawaan, supaya halaman tidak gagal karena tautan
// yang salah ketik.

export function searchSortFromParam(value: string | null): SearchSort {
  return SEARCH_SORTS.find((s) => s.value === value)?.value ?? 'relevance'
}

export function durationFromParam(value: string | null): DurationFilter {
  return DURATIONS.find((d) => d.value === value)?.value ?? ''
}

export function channelSortFromParam(value: string | null): ChannelSort {
  return value === 'popular' ? 'popular' : 'newest'
}

/**
 * Mengubah satu parameter URL dan membiarkan yang lain. Nilai bawaan
 * dihapus dari URL, supaya tautan tetap pendek (/search?q=es, bukan
 * /search?q=es&sort=relevance).
 */
export function withParam(params: URLSearchParams, name: string, value: string, defaultValue = ''): URLSearchParams {
  const next = new URLSearchParams(params)
  if (value === defaultValue) next.delete(name)
  else next.set(name, value)
  return next
}

/** Nama pemilik video untuk ditampilkan (sama dengan VideoCard). */
export function ownerName(v: Video): string {
  return v.owner?.display_name || v.owner?.username || 'Channel'
}
