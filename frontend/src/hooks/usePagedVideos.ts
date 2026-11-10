import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { errorMessage, isAbortError } from '../api/client'
import type { Video, VideoPage } from '../api/types'

type Fetcher = (pageToken: string, signal: AbortSignal) => Promise<VideoPage>

export interface PagedVideos {
  videos: Video[]
  loading: boolean
  error: string | null
  hasMore: boolean
  loadMore: () => void
  retry: () => void
}

interface PageState {
  /** Daftar ini milik key yang mana (lihat di bawah). */
  key: string
  videos: Video[]
  nextToken: string
  loading: boolean
  error: string | null
}

/**
 * Mengambil daftar video halaman demi halaman memakai next_page_token
 * dari API. Jika key berubah (misalnya kata kunci pencarian), daftar
 * dimulai lagi dari halaman pertama.
 *
 * Library seperti TanStack Query (useInfiniteQuery) melakukan hal yang sama
 * plus cache; untuk sekarang hook kecil ini cukup.
 */
export function usePagedVideos(key: string, fetchPage: Fetcher): PagedVideos {
  const [reloads, setReloads] = useState(0)
  const fullKey = `${key}#${reloads}`
  const [state, setState] = useState<PageState | null>(null)
  const controller = useRef<AbortController | null>(null)

  // Simpan fungsi terbaru di ref, supaya effect tidak berjalan ulang setiap
  // render (fetchPage biasanya fungsi inline yang baru setiap render).
  const fetchRef = useRef(fetchPage)
  useLayoutEffect(() => {
    fetchRef.current = fetchPage
  })

  const run = useCallback((forKey: string, pageToken: string, previous: Video[]) => {
    controller.current?.abort()
    const ctrl = new AbortController()
    controller.current = ctrl
    fetchRef
      .current(pageToken, ctrl.signal)
      .then((page) => {
        setState({
          key: forKey,
          videos: [...previous, ...page.videos],
          nextToken: page.next_page_token,
          loading: false,
          error: null,
        })
      })
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        setState({ key: forKey, videos: previous, nextToken: pageToken, loading: false, error: errorMessage(err) })
      })
  }, [])

  // Halaman pertama, setiap kali key berubah.
  useEffect(() => {
    run(fullKey, '', [])
    return () => controller.current?.abort()
  }, [fullKey, run])

  // Selama halaman pertama untuk key baru belum datang, state masih milik
  // key lama: anggap kosong dan sedang memuat.
  const current = state?.key === fullKey ? state : null

  const loadMore = useCallback(() => {
    if (!current || current.loading || current.error || !current.nextToken) return
    setState({ ...current, loading: true })
    run(fullKey, current.nextToken, current.videos)
  }, [current, fullKey, run])

  const retry = useCallback(() => {
    if (current && current.videos.length > 0) {
      setState({ ...current, loading: true, error: null })
      run(fullKey, current.nextToken, current.videos)
    } else {
      setReloads((n) => n + 1)
    }
  }, [current, fullKey, run])

  return {
    videos: current?.videos ?? [],
    loading: current ? current.loading : true,
    error: current?.error ?? null,
    hasMore: Boolean(current?.nextToken),
    loadMore,
    retry,
  }
}
