import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { listLiveChat, liveChatUrl } from '../api/endpoints'
import type { LiveChatMessage } from '../api/types'
import { appendChat } from '../lib/live'

export interface LiveChat {
  /** Pesan yang sudah dimuat, urut dari yang paling lama. */
  messages: LiveChatMessage[]
  /** false selama pesan awal belum termuat. */
  loaded: boolean
  /** Menambahkan pesan (misalnya milik sendiri yang baru terkirim). */
  add: (m: LiveChatMessage) => void
}

/**
 * Chat sebuah siaran (langkah 25c): 50 pesan terakhir saat halaman dibuka,
 * lalu pesan baru lewat Server-Sent Events, sama seperti danmaku video.
 * onMessage dipanggil untuk setiap pesan baru dari SSE (untuk danmaku
 * melayang di atas video).
 *
 * live=false (siaran sudah selesai): hanya rekaman chat, tanpa SSE.
 *
 * State tidak dikosongkan saat streamId berubah: halaman tonton live
 * memasang komponennya dengan key={id}, jadi pindah siaran = hook baru.
 */
export function useLiveChat(streamId: string, live: boolean, onMessage?: (m: LiveChatMessage) => void): LiveChat {
  const [messages, setMessages] = useState<LiveChatMessage[]>([])
  const [loaded, setLoaded] = useState(false)
  const onMessageRef = useRef(onMessage)
  useLayoutEffect(() => {
    onMessageRef.current = onMessage
  })

  const add = useCallback((m: LiveChatMessage) => setMessages((l) => appendChat(l, [m])), [])

  useEffect(() => {
    const ctrl = new AbortController()
    listLiveChat(streamId, ctrl.signal)
      .then((list) => setMessages((l) => appendChat(list, l)))
      // Gagal (atau dibatalkan): chat tetap jalan, hanya tanpa pesan lama.
      .catch(() => {})
      .finally(() => {
        if (!ctrl.signal.aborted) setLoaded(true)
      })
    return () => ctrl.abort()
  }, [streamId])

  // https://developer.mozilla.org/en-US/docs/Web/API/EventSource
  // EventSource menyambung ulang sendiri jika koneksi putus.
  useEffect(() => {
    if (!live) return
    const source = new EventSource(liveChatUrl(streamId))
    const onChat = (e: MessageEvent<string>) => {
      try {
        const m = JSON.parse(e.data) as LiveChatMessage
        setMessages((l) => appendChat(l, [m]))
        onMessageRef.current?.(m)
      } catch {
        // Data rusak: abaikan satu event ini saja.
      }
    }
    source.addEventListener('chat', onChat)
    return () => {
      source.removeEventListener('chat', onChat)
      source.close()
    }
  }, [streamId, live])

  return { messages, loaded, add }
}
