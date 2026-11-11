import { useCallback, useEffect, useState } from 'react'
import { ApiError, errorMessage, isAbortError } from '../api/client'
import { getChannel } from '../api/endpoints'
import type { ChannelDetail, SubscriptionResult } from '../api/types'
import { useAuth } from '../auth/useAuth'

export interface ChannelState {
  /** null selama memuat atau jika gagal. */
  channel: ChannelDetail | null
  loading: boolean
  error: { message: string; notFound: boolean } | null
  /** Penonton sedang melihat kanalnya sendiri. */
  isOwn: boolean
  retry: () => void
  /** Memasukkan hasil tombol Ikuti, supaya angka pengikut ikut berubah. */
  applySubscription: (r: SubscriptionResult) => void
}

interface Loaded {
  key: string
  channel?: ChannelDetail
  error?: { message: string; notFound: boolean }
}

/**
 * Memuat profil kanal dari GET /users/:username. Menunggu pengecekan login
 * selesai lebih dulu, supaya request membawa access token dan server ikut
 * menjawab viewer_subscribed. Dimuat ulang saat user login atau logout.
 */
export function useChannel(username: string): ChannelState {
  const { status, user } = useAuth()
  const [reloads, setReloads] = useState(0)
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const key = `${username}|${user?.id ?? ''}|${reloads}`

  useEffect(() => {
    if (status === 'loading' || !username) return
    const ctrl = new AbortController()
    getChannel(username, ctrl.signal)
      .then((channel) => setLoaded({ key, channel }))
      .catch((err: unknown) => {
        if (isAbortError(err)) return
        const notFound = err instanceof ApiError && err.status === 404
        setLoaded({ key, error: { message: errorMessage(err), notFound } })
      })
    return () => ctrl.abort()
  }, [username, key, status])

  // Hasil milik key lama (kanal lain, atau sebelum login) dianggap belum ada.
  const current = status !== 'loading' && loaded?.key === key ? loaded : null

  const applySubscription = useCallback(
    (r: SubscriptionResult) => {
      setLoaded((prev) =>
        prev?.key === key && prev.channel
          ? {
              ...prev,
              channel: {
                user: { ...prev.channel.user, subscriber_count: r.subscriber_count },
                viewer_subscribed: r.subscribed,
              },
            }
          : prev,
      )
    },
    [key],
  )

  const retry = useCallback(() => setReloads((n) => n + 1), [])

  return {
    channel: current?.channel ?? null,
    loading: current === null,
    error: current?.error ?? null,
    isOwn: Boolean(user && current?.channel && user.id === current.channel.user.id),
    retry,
    applySubscription,
  }
}
