import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { getUnreadCount } from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { UnreadContext, type UnreadState } from './context'

/** Angka di lonceng diperbarui setiap 60 detik, sesuai saran di backend. */
export const UNREAD_POLL_MS = 60_000

/**
 * Menyimpan jumlah notifikasi belum dibaca untuk lonceng di header.
 *
 * Angkanya diambil dengan polling (bertanya berkala), bukan push. Backend
 * kita belum punya SSE/WebSocket untuk notifikasi, dan satu request kecil
 * per menit per tab cukup ringan. Polling berhenti saat tab tidak terlihat
 * (Page Visibility API) dan langsung bertanya lagi saat tab dibuka kembali.
 * https://developer.mozilla.org/en-US/docs/Web/API/Page_Visibility_API
 */
export function UnreadProvider({ children }: { children: ReactNode }) {
  const { status, user } = useAuth()
  const userId = status === 'authenticated' ? (user?.id ?? null) : null
  // Angka disimpan bersama id user-nya, supaya angka akun lama tidak
  // terbawa setelah logout atau ganti akun.
  const [state, setState] = useState<{ userId: string; count: number } | null>(null)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    if (!userId) return
    const ctrl = new AbortController()
    const load = () => {
      if (document.visibilityState !== 'visible') return
      getUnreadCount(ctrl.signal)
        .then((count) => setState({ userId, count }))
        // Gagal sesekali tidak apa-apa: angka lama tetap tampil, dicoba lagi nanti.
        .catch(() => {})
    }
    load()
    const timer = window.setInterval(load, UNREAD_POLL_MS)
    document.addEventListener('visibilitychange', load)
    return () => {
      ctrl.abort()
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', load)
    }
  }, [userId, tick])

  const setCount = useCallback(
    (count: number) => {
      if (userId) setState({ userId, count })
    },
    [userId],
  )
  const refresh = useCallback(() => setTick((n) => n + 1), [])

  const count = userId && state?.userId === userId ? state.count : 0
  const value = useMemo<UnreadState>(() => ({ count, setCount, refresh }), [count, setCount, refresh])
  return <UnreadContext value={value}>{children}</UnreadContext>
}
