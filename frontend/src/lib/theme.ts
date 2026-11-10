// Mode gelap/terang. Nilai disimpan di atribut <html data-theme="...">;
// semua warna di index.css dibaca dari variabel CSS yang berganti
// mengikuti atribut ini. Skrip kecil di index.html memasangnya sebelum
// React jalan, jadi tidak ada kedipan warna saat halaman dibuka.

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'ice-theme'

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // Mode private / storage diblokir: tema tetap berlaku untuk tab ini.
  }
}
