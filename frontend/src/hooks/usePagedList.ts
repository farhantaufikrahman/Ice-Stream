import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { errorMessage, isAbortError } from '../api/client'

export interface Page<T> {
  items: T[]
  /** "" jika tidak ada halaman berikutnya. */
  next: string
}

export interface PagedList<T> {
  items: T[]
  loading: boolean
  error: string | null
  hasMore: boolean
  loadMore: () => void
  retry: () => void
  /** Mengubah daftar di layar tanpa request (misalnya komentar baru ditambahkan di atas). */
  update: (fn: (items: T[]) => T[]) => void
}

interface State<T> {
  items: T[]
  next: string
  loading: boolean
  error: string | null
}

/**
 * Daftar berhalaman yang bisa diubah setelah dimuat (tambah/hapus satu
 * item). Halaman pertama dimuat saat komponen muncul. Untuk daftar yang
 * harus diulang dari awal (misalnya pindah video), beri komponennya `key`
 * baru supaya hook ini mulai ulang.
 *
 * Bedanya dengan usePagedVideos: daftar di sini bisa diubah lewat update(),
 * yang dibutuhkan komentar (komentar baru langsung tampil tanpa memuat ulang).
 */
export function usePagedList<T>(fetchPage: (pageToken: string, signal: AbortSignal) => Promise<Page<T>>): PagedList<T> {
  const [state, setState] = useState<State<T>>({ items: [], next: '', loading: true, error: null })
  const controller = useRef<AbortController | null>(null)
  const fetchRef = useRef(fetchPage)
  useLayoutEffect(() => {
    fetchRef.current = fetchPage
  })

  const run = useCallback((pageToken: string) => {
    controller.current?.abort()
    const ctrl = new AbortController()
    controller.current = ctrl
    fetchRef
      .current(pageToken, ctrl.signal)
      .then((page) => {
        setState((s) => ({
          items: pageToken ? [...s.items, ...page.items] : page.items,
          next: page.next,
          loading: false,
          error: null,
        }))
      })
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        setState((s) => ({ ...s, loading: false, error: errorMessage(err) }))
      })
  }, [])

  useEffect(() => {
    run('')
    return () => controller.current?.abort()
  }, [run])

  const loadMore = useCallback(() => {
    if (state.loading || state.error || !state.next) return
    setState((s) => ({ ...s, loading: true }))
    run(state.next)
  }, [state.loading, state.error, state.next, run])

  const retry = useCallback(() => {
    setState((s) => ({ ...s, loading: true, error: null }))
    // Halaman yang gagal diulang: halaman pertama jika daftar masih kosong.
    run(state.items.length > 0 ? state.next : '')
  }, [state.items.length, state.next, run])

  const update = useCallback((fn: (items: T[]) => T[]) => {
    setState((s) => ({ ...s, items: fn(s.items) }))
  }, [])

  return {
    items: state.items,
    loading: state.loading,
    error: state.error,
    hasMore: Boolean(state.next),
    loadMore,
    retry,
    update,
  }
}
