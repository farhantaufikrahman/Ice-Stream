/**
 * Panjang garis progres di thumbnail riwayat, 0 sampai 1. Durasi bisa
 * kosong (video lama) atau posisi sedikit melewati durasi (pembulatan),
 * jadi hasilnya dijepit.
 */
export function watchedFraction(positionMs: number, durationMs?: number): number {
  if (!durationMs || durationMs <= 0 || positionMs <= 0) return 0
  return Math.min(1, positionMs / durationMs)
}
