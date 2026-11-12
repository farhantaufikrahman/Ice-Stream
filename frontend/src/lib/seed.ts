/**
 * Seed acak untuk "Rekomendasi untukmu" (langkah 27): angka 64-bit sebagai
 * teks, karena angka JavaScript biasa hanya tepat sampai 2^53.
 *
 * Seed disimpan di sessionStorage, jadi memuat ulang halaman (F5) atau
 * kembali dari halaman tonton memberi susunan yang sama. Tombol "Muat
 * ulang" membuat seed baru.
 */
const KEY = 'ice-stream:recommend-seed'

export function newSeed(): string {
  const [hi, lo] = crypto.getRandomValues(new Uint32Array(2))
  return ((BigInt(hi) << 32n) | BigInt(lo)).toString()
}

/** Seed tab ini; dibuat jika belum ada. sessionStorage bisa diblokir browser, jadi selalu dibungkus try. */
export function sessionSeed(): string {
  try {
    const saved = sessionStorage.getItem(KEY)
    if (saved && /^\d{1,20}$/.test(saved)) return saved
  } catch {
    // Diblokir: pakai seed baru setiap kali.
  }
  return rememberSeed(newSeed())
}

export function rememberSeed(seed: string): string {
  try {
    sessionStorage.setItem(KEY, seed)
  } catch {
    // Diblokir: tidak apa-apa, hanya tidak diingat.
  }
  return seed
}
