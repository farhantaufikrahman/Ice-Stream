// Pilihan "Danmaku aktif/mati" diingat per browser, dipakai bersama oleh
// halaman tonton video dan halaman tonton siaran langsung (langkah 25c).

const DANMAKU_PREF_KEY = 'ice-danmaku'

export function readDanmakuPref(): boolean {
  try {
    return localStorage.getItem(DANMAKU_PREF_KEY) !== 'off'
  } catch {
    // localStorage bisa diblokir (mode privat tertentu): pakai default.
    return true
  }
}

export function saveDanmakuPref(on: boolean): void {
  try {
    localStorage.setItem(DANMAKU_PREF_KEY, on ? 'on' : 'off')
  } catch {
    // Tidak tersimpan pun tidak apa-apa.
  }
}
