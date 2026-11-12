import { useEffect, useState } from 'react'
import { errorMessage, isAbortError } from '../api/client'
import { listMyVideos } from '../api/endpoints'
import type { Video } from '../api/types'

/** Batas halaman yang diambil untuk pilihan video (24 per halaman). */
const MAX_PAGES = 10

export interface VideoOptions {
  videos: Pick<Video, 'id' | 'title'>[]
  error: string | null
  /** true jika video channel lebih banyak dari yang dimuat. */
  truncated: boolean
}

/**
 * Daftar video milik user untuk pilihan "Semua video / satu video" di
 * halaman Analitik, Komentar, dan Danmaku Creator Studio (langkah 26).
 */
export function useMyVideoOptions(userId: string): VideoOptions {
  const [state, setState] = useState<VideoOptions & { key: string }>({ key: '', videos: [], error: null, truncated: false })

  useEffect(() => {
    if (!userId) return
    const ctrl = new AbortController()
    const load = async () => {
      const all: Pick<Video, 'id' | 'title'>[] = []
      let token = ''
      for (let page = 0; page < MAX_PAGES; page++) {
        const res = await listMyVideos(token, ctrl.signal)
        all.push(...res.videos.map((v) => ({ id: v.id, title: v.title })))
        token = res.next_page_token
        if (!token) break
      }
      setState({ key: userId, videos: all, error: null, truncated: token !== '' })
    }
    load().catch((err: unknown) => {
      if (!isAbortError(err)) setState({ key: userId, videos: [], error: errorMessage(err), truncated: false })
    })
    return () => ctrl.abort()
  }, [userId])

  return state.key === userId ? state : { videos: [], error: null, truncated: false }
}
