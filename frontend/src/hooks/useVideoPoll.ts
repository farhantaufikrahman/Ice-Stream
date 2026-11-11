import { useEffect, useLayoutEffect, useRef } from 'react'
import { isAbortError } from '../api/client'
import { getVideo } from '../api/endpoints'
import type { Video } from '../api/types'

/** Jeda antar-pengecekan status. Transcoding biasanya butuh puluhan detik. */
const POLL_INTERVAL_MS = 4000

/**
 * Mengecek ulang data satu video secara berkala (GET /videos/:id) selama
 * `enabled` true, misalnya saat statusnya masih UPLOADED/PROCESSING.
 * Berhenti sendiri saat komponen ditutup atau `enabled` menjadi false.
 *
 * Polling dipilih karena paling sederhana. Alternatifnya Server-Sent Events
 * atau WebSocket (server yang mengabari), seperti yang dipakai danmaku live.
 * Library seperti TanStack Query punya opsi refetchInterval untuk hal ini.
 */
export function useVideoPoll(id: string | null, enabled: boolean, onUpdate: (video: Video) => void): void {
  const callback = useRef(onUpdate)
  useLayoutEffect(() => {
    callback.current = onUpdate
  })

  useEffect(() => {
    if (!id || !enabled) return
    const ctrl = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      try {
        const detail = await getVideo(id, ctrl.signal)
        callback.current(detail.video)
      } catch (err) {
        if (isAbortError(err)) return
        // Gagal sesekali (misalnya jaringan) tidak apa-apa: coba lagi nanti.
      }
      // setTimeout berantai (bukan setInterval): request berikutnya baru
      // dikirim setelah yang sebelumnya selesai, jadi tidak menumpuk.
      if (!ctrl.signal.aborted) timer = setTimeout(tick, POLL_INTERVAL_MS)
    }
    timer = setTimeout(tick, POLL_INTERVAL_MS)
    return () => {
      clearTimeout(timer)
      ctrl.abort()
    }
  }, [id, enabled])
}
