import { useEffect, useState } from 'react'
import { errorMessage, isAbortError } from '../api/client'
import { listTrending } from '../api/endpoints'
import type { TrendingVideo } from '../api/types'

interface TrendingState {
  /** Untuk kombinasi category|limit|reloads yang mana daftar ini diambil. */
  key: string
  videos: TrendingVideo[]
  error: string | null
}

export interface Trending {
  /** null selama memuat. */
  videos: TrendingVideo[] | null
  error: string | null
  retry: () => void
}

/**
 * Mengambil daftar Trending sekali (tanpa halaman berikutnya: paling
 * banyak 50 video). Saat kategori berubah, permintaan lama dibatalkan
 * dengan AbortController supaya hasil kategori lama tidak menimpa yang baru.
 */
export function useTrending(category: string, limit: number): Trending {
  const [reloads, setReloads] = useState(0)
  const key = `${category}|${limit}|${reloads}`
  const [state, setState] = useState<TrendingState | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    listTrending(category, limit, ctrl.signal)
      .then((r) => setState({ key, videos: r.videos, error: null }))
      .catch((err: unknown) => {
        if (!isAbortError(err)) setState({ key, videos: [], error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [category, limit, key])

  // Hasil milik key lama dianggap "masih memuat", jadi tidak perlu
  // setState di awal effect (aturan react-hooks/set-state-in-effect).
  const current = state?.key === key ? state : null
  return {
    videos: current ? current.videos : null,
    error: current?.error ?? null,
    retry: () => setReloads((n) => n + 1),
  }
}
