/** Halaman informasi dari tautan footer (desain Home.dc.html), langkah 27. */
export type InfoTopic = 'tentang' | 'pedoman' | 'privasi' | 'bantuan'

export const INFO_LINKS: { topic: InfoTopic; path: string; label: string }[] = [
  { topic: 'tentang', path: '/tentang', label: 'Tentang' },
  { topic: 'pedoman', path: '/pedoman-komunitas', label: 'Pedoman komunitas' },
  { topic: 'privasi', path: '/privasi', label: 'Privasi' },
  { topic: 'bantuan', path: '/bantuan', label: 'Bantuan' },
]
