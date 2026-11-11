import { useCallback, useEffect, useState } from 'react'
import { errorMessage, isAbortError } from '../api/client'
import { getPlaylist, listPlaylistVideos, MAX_PAGE_SIZE } from '../api/endpoints'
import type { Playlist, PlaylistEntry } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { queuePosition, type QueuePosition } from '../lib/playlistQueue'

export interface PlaylistQueue extends QueuePosition {
  playlist: Playlist | null
  entries: PlaylistEntry[]
  loading: boolean
  error: string | null
  retry: () => void
}

interface State {
  key: string
  playlist: Playlist | null
  entries: PlaylistEntry[]
  next: string
  error: string | null
}

/**
 * Antrean untuk menonton playlist (/watch/:id?list=...): data playlist dan
 * isinya. Dipakai di WatchPage (bukan di dalam tampilan satu video),
 * supaya antrean tidak dimuat ulang setiap kali pindah ke video berikutnya.
 *
 * Halaman berikutnya dimuat otomatis hanya jika dibutuhkan: video yang
 * sedang diputar belum ditemukan, atau video itu yang terakhir dimuat.
 * Server membatasi 1000 video per playlist, jadi paling banyak 20 request.
 */
export function usePlaylistQueue(listId: string | null, videoId: string): PlaylistQueue | null {
  const { status, user } = useAuth()
  const [reloads, setReloads] = useState(0)
  const key = `${listId ?? ''}|${user?.id ?? ''}|${reloads}`
  const [state, setState] = useState<State | null>(null)

  // Halaman pertama dan data playlist. Menunggu pengecekan login, supaya
  // playlist PRIVATE milik sendiri bisa dibuka.
  useEffect(() => {
    if (!listId || status === 'loading') return
    const ctrl = new AbortController()
    Promise.all([getPlaylist(listId, ctrl.signal), listPlaylistVideos(listId, '', ctrl.signal, MAX_PAGE_SIZE)])
      .then(([playlist, page]) =>
        setState({ key, playlist, entries: page.entries, next: page.next_page_token, error: null }),
      )
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        setState({ key, playlist: null, entries: [], next: '', error: errorMessage(err) })
      })
    return () => ctrl.abort()
  }, [listId, key, status])

  // Hasil milik key lama (playlist lain, atau sebelum login) dianggap
  // belum ada, sama seperti useChannel. Selama itu loading = true.
  const current = state?.key === key ? state : null
  const position = queuePosition(current?.entries ?? [], videoId, Boolean(current?.next))
  const needMore = Boolean(current && !current.error && position.needMore)
  const pageToken = current?.next ?? ''

  // Halaman berikutnya, hanya jika dibutuhkan (lihat komentar di atas).
  useEffect(() => {
    if (!listId || !needMore) return
    const ctrl = new AbortController()
    listPlaylistVideos(listId, pageToken, ctrl.signal, MAX_PAGE_SIZE)
      .then((page) =>
        setState((s) =>
          s && s.key === key ? { ...s, entries: [...s.entries, ...page.entries], next: page.next_page_token } : s,
        ),
      )
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        setState((s) => (s && s.key === key ? { ...s, error: errorMessage(err) } : s))
      })
    return () => ctrl.abort()
  }, [listId, needMore, pageToken, key])

  const retry = useCallback(() => setReloads((n) => n + 1), [])

  if (!listId) return null
  return {
    playlist: current?.playlist ?? null,
    entries: current?.entries ?? [],
    loading: current === null,
    error: current?.error ?? null,
    retry,
    ...position,
  }
}
