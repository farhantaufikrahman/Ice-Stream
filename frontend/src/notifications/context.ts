import { createContext } from 'react'

export interface UnreadState {
  /** Jumlah notifikasi belum dibaca (0 untuk tamu). Server berhenti menghitung di 100. */
  count: number
  /** Memasang angka dari jawaban server (misalnya unread_count setelah tandai dibaca). */
  setCount: (n: number) => void
  /** Minta angka terbaru ke server sekarang. */
  refresh: () => void
}

export const UnreadContext = createContext<UnreadState | null>(null)
