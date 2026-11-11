import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { isAbortError } from '../api/client'
import { danmakuLiveUrl, listDanmaku } from '../api/endpoints'
import type { Danmaku } from '../api/types'
import { DANMAKU_WINDOW_MS, mergeDanmaku, windowIndex } from '../lib/danmaku'

/** Potongan berikutnya mulai diambil saat posisi tinggal 30 detik dari batasnya. */
const PREFETCH_MS = 30_000

export interface DanmakuTrack {
  /** Semua danmaku yang sudah dimuat, urut posisi. */
  list: Danmaku[]
  /** Menambahkan danmaku (misalnya milik sendiri yang baru terkirim). */
  add: (d: Danmaku) => void
}

/**
 * Mengambil danmaku sebuah video per potongan 6 menit, sesuai posisi
 * video yang sedang diputar (seperti Bilibili), lalu mendengarkan danmaku
 * baru dari penonton lain lewat Server-Sent Events.
 *
 * onLive dipanggil untuk setiap danmaku live, supaya bisa langsung tampil
 * di layar walau posisinya bukan posisi video kita sekarang.
 */
export function useDanmaku(
  videoId: string,
  videoRef: RefObject<HTMLVideoElement | null>,
  enabled: boolean,
  onLive?: (d: Danmaku) => void,
): DanmakuTrack {
  const [list, setList] = useState<Danmaku[]>([])
  const liveRef = useRef(onLive)
  useLayoutEffect(() => {
    liveRef.current = onLive
  })

  const add = useCallback((d: Danmaku) => setList((l) => mergeDanmaku(l, [d])), [])

  // Rekaman: potongan 6 menit di sekitar posisi video.
  useEffect(() => {
    const video = videoRef.current
    if (!enabled || !video) return
    const requested = new Set<number>()
    const ctrl = new AbortController()

    const load = async (index: number) => {
      if (requested.has(index)) return
      requested.add(index)
      const from = index * DANMAKU_WINDOW_MS
      // Batas atas dikurangi 1 ms supaya danmaku tepat di 06:00 tidak
      // terambil dua kali (rentang server termasuk kedua ujungnya).
      const to = from + DANMAKU_WINDOW_MS - 1
      let token = ''
      try {
        do {
          const page = await listDanmaku(videoId, from, to, token, ctrl.signal)
          setList((l) => mergeDanmaku(l, page.danmaku))
          token = page.next_page_token
        } while (token)
      } catch (err) {
        if (isAbortError(err)) return
        // Gagal: boleh dicoba lagi saat posisi video berubah berikutnya.
        requested.delete(index)
      }
    }

    const check = () => {
      const ms = video.currentTime * 1000
      const index = windowIndex(ms)
      void load(index)
      if ((index + 1) * DANMAKU_WINDOW_MS - ms < PREFETCH_MS && (!video.duration || (index + 1) * DANMAKU_WINDOW_MS < video.duration * 1000)) {
        void load(index + 1)
      }
    }
    check()
    video.addEventListener('timeupdate', check)
    video.addEventListener('seeked', check)
    return () => {
      video.removeEventListener('timeupdate', check)
      video.removeEventListener('seeked', check)
      ctrl.abort()
    }
  }, [videoId, videoRef, enabled])

  // Live: setiap danmaku baru dari penonton lain.
  // https://developer.mozilla.org/en-US/docs/Web/API/EventSource
  // EventSource menyambung ulang sendiri jika koneksi putus. Danmaku yang
  // terkirim selama putus tidak hilang: tetap ada di rekaman.
  useEffect(() => {
    if (!enabled) return
    const source = new EventSource(danmakuLiveUrl(videoId))
    const onDanmaku = (e: MessageEvent<string>) => {
      try {
        const d = JSON.parse(e.data) as Danmaku
        setList((l) => mergeDanmaku(l, [d]))
        liveRef.current?.(d)
      } catch {
        // Data rusak: abaikan satu event ini saja.
      }
    }
    source.addEventListener('danmaku', onDanmaku)
    return () => {
      source.removeEventListener('danmaku', onDanmaku)
      source.close()
    }
  }, [videoId, enabled])

  return { list, add }
}
