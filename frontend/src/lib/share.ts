// Langkah 24b: tautan Bagikan dan parameter ?t= (mulai dari detik ke-N).

/** Batas wajar ?t=: 24 jam. Angka lebih besar dianggap salah ketik. */
const MAX_START_SECONDS = 24 * 3600

/**
 * Tautan untuk dibagikan, misalnya https://situs/watch/42?t=83.
 * startSeconds 0 (atau kurang) berarti tanpa ?t=, video mulai dari awal.
 */
export function shareUrl(origin: string, videoId: string, startSeconds = 0): string {
  const url = new URL(`/watch/${encodeURIComponent(videoId)}`, origin)
  const t = Math.floor(startSeconds)
  if (t > 0) url.searchParams.set('t', String(t))
  return url.toString()
}

/**
 * Membaca ?t= dari alamat, dalam detik. Menerima angka detik ("83") dan
 * bentuk seperti YouTube ("1m23s", "1h2m", "45s"). Nilai lain, termasuk
 * kosong, menjadi 0 (mulai dari awal), jadi tautan rusak tetap bisa diputar.
 */
export function parseStartParam(value: string | null): number {
  if (!value) return 0
  let seconds: number
  if (/^\d+$/.test(value)) {
    seconds = Number(value)
  } else {
    const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(value)
    if (!m || value === '') return 0
    seconds = Number(m[1] ?? 0) * 3600 + Number(m[2] ?? 0) * 60 + Number(m[3] ?? 0)
  }
  return seconds > 0 && seconds <= MAX_START_SECONDS ? seconds : 0
}
