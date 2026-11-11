import { useCallback, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState, type CSSProperties, type Ref, type RefObject } from 'react'
import type { Danmaku } from '../../api/types'
import { estimateTextWidth, firstAfter, FIXED_DURATION_MS, laneFits, SCROLL_DURATION_MS, type LaneItem } from '../../lib/danmaku'

const FONT_SIZE = 22
const LINE_HEIGHT = 30
/** Batas jumlah danmaku di layar sekaligus, supaya browser tetap ringan. */
const MAX_ACTIVE = 120
/** Jarak dari bawah untuk danmaku BOTTOM, supaya tidak menutupi kontrol video. */
const BOTTOM_OFFSET = 56

export interface DanmakuLayerHandle {
  /** Tampilkan sekarang juga (danmaku live atau milik sendiri). */
  show: (d: Danmaku, mine?: boolean) => void
}

interface Active {
  key: string
  d: Danmaku
  lane: number
  mine: boolean
  /** Lebar layer saat danmaku ini mulai (px), untuk titik awal animasi. */
  from: number
}

interface Props {
  ref?: Ref<DanmakuLayerHandle>
  videoRef: RefObject<HTMLVideoElement | null>
  /** Semua danmaku yang sudah dimuat, urut posisi. */
  items: Danmaku[]
  visible: boolean
}

/**
 * Lapisan danmaku di atas <video>. Setiap danmaku yang posisinya baru
 * dilewati video ditampilkan sebagai <span> dengan animasi CSS:
 *   - SCROLL: berjalan dari kanan ke kiri selama 8 detik;
 *   - TOP / BOTTOM: diam di tengah atas/bawah selama 4 detik.
 * Animasi ikut berhenti saat video dijeda (animation-play-state).
 *
 * Posisi video dibaca dengan requestAnimationFrame (sekitar 60 kali per
 * detik) karena event timeupdate hanya sekitar 4 kali per detik, terlalu
 * kasar untuk danmaku. State React hanya diubah saat ada danmaku yang
 * masuk atau keluar, bukan setiap frame.
 * https://developer.mozilla.org/en-US/docs/Web/API/Window/requestAnimationFrame
 */
export function DanmakuLayer({ ref, videoRef, items, visible }: Props) {
  const layerRef = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState<Active[]>([])
  const [paused, setPaused] = useState(true)

  // Data yang berubah tiap frame disimpan di ref (bukan state) supaya
  // tidak memicu render ulang.
  const engine = useRef({
    lastMs: 0,
    shown: new Set<string>(),
    scrollLanes: [] as (LaneItem | null)[],
    topUntil: [] as number[],
    bottomUntil: [] as number[],
    count: 0,
    items,
    visible,
  })
  useLayoutEffect(() => {
    engine.current.items = items
    engine.current.visible = visible
  }, [items, visible])

  const reset = useCallback((ms: number) => {
    const e = engine.current
    e.lastMs = ms
    e.shown.clear()
    e.scrollLanes = []
    e.topUntil = []
    e.bottomUntil = []
    setActive([])
  }, [])

  /** Mencari jalur kosong lalu menampilkan danmaku. false jika layar penuh. */
  const place = useCallback((d: Danmaku, nowMs: number, mine: boolean): boolean => {
    const layer = layerRef.current
    const e = engine.current
    if (!layer || e.shown.has(d.id)) return false
    const width = layer.clientWidth
    const height = layer.clientHeight
    if (width === 0 || height === 0) return false
    let lane = -1

    if (d.mode === 'SCROLL') {
      // Danmaku berjalan memakai 80% tinggi atas, supaya bagian bawah
      // (kontrol video) tetap terlihat.
      const lanes = Math.max(1, Math.floor((height * 0.8) / LINE_HEIGHT))
      const textWidth = estimateTextWidth(d.body, FONT_SIZE) + (mine ? 16 : 0)
      for (let i = 0; i < lanes; i++) {
        if (laneFits(e.scrollLanes[i] ?? null, nowMs, textWidth, width, SCROLL_DURATION_MS)) {
          lane = i
          e.scrollLanes[i] = { start: nowMs, width: textWidth }
          break
        }
      }
    } else {
      const lanes = Math.max(1, Math.floor((height * 0.5) / LINE_HEIGHT))
      const until = d.mode === 'TOP' ? e.topUntil : e.bottomUntil
      for (let i = 0; i < lanes; i++) {
        if ((until[i] ?? 0) <= nowMs) {
          lane = i
          until[i] = nowMs + FIXED_DURATION_MS
          break
        }
      }
    }
    if (lane < 0) return false // Semua jalur terisi: danmaku dilewati, seperti di Bilibili.

    e.shown.add(d.id)
    e.count++
    const item: Active = { key: `${d.id}-${e.count}`, d, lane, mine, from: width }
    setActive((list) => (list.length >= MAX_ACTIVE ? [...list.slice(1), item] : [...list, item]))
    return true
  }, [])

  useImperativeHandle(
    ref,
    () => ({
      show: (d, mine = false) => {
        const video = videoRef.current
        if (mine && engine.current.shown.has(d.id)) {
          // Event live (SSE) untuk danmaku kita sendiri bisa tiba lebih dulu
          // daripada jawaban POST. Danmaku itu sudah tampil: cukup beri tanda.
          setActive((list) => list.map((a) => (a.d.id === d.id ? { ...a, mine: true } : a)))
          return
        }
        if (!video || !engine.current.visible) {
          engine.current.shown.add(d.id)
          return
        }
        place(d, video.currentTime * 1000, mine)
      },
    }),
    [place, videoRef],
  )

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    let frame = 0

    const tick = () => {
      const e = engine.current
      const now = video.currentTime * 1000
      // Lompatan besar (seek, atau tab lama tidak aktif): jangan tampilkan
      // semua danmaku yang terlewat sekaligus, mulai lagi dari posisi baru.
      if (now < e.lastMs - 500 || now > e.lastMs + 1500) {
        reset(now)
      } else if (now > e.lastMs) {
        if (e.visible) {
          const list = e.items
          for (let i = firstAfter(list, e.lastMs); i < list.length && list[i].position_ms <= now; i++) {
            place(list[i], now, false)
          }
        }
        e.lastMs = now
      }
      if (!video.paused) frame = requestAnimationFrame(tick)
    }

    const onPlay = () => {
      setPaused(false)
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(tick)
    }
    const onPause = () => {
      setPaused(true)
      cancelAnimationFrame(frame)
    }
    const onSeeking = () => reset(video.currentTime * 1000)

    engine.current.lastMs = video.currentTime * 1000
    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onPause)
    video.addEventListener('ended', onPause)
    video.addEventListener('seeking', onSeeking)
    return () => {
      cancelAnimationFrame(frame)
      video.removeEventListener('play', onPlay)
      video.removeEventListener('pause', onPause)
      video.removeEventListener('ended', onPause)
      video.removeEventListener('seeking', onSeeking)
    }
  }, [videoRef, place, reset])

  const remove = (key: string) => setActive((list) => list.filter((a) => a.key !== key))

  return (
    <div ref={layerRef} className={paused ? 'danmaku-layer is-paused' : 'danmaku-layer'} aria-hidden="true" hidden={!visible}>
      {visible &&
        active.map((a) => {
          const style: CSSProperties & Record<'--dm-from', string> = {
            color: a.d.color,
            fontSize: FONT_SIZE,
            lineHeight: `${LINE_HEIGHT}px`,
            '--dm-from': `${a.from}px`,
          }
          if (a.d.mode === 'BOTTOM') style.bottom = BOTTOM_OFFSET + a.lane * LINE_HEIGHT
          else style.top = 6 + a.lane * LINE_HEIGHT
          const mode = a.d.mode === 'SCROLL' ? 'dm-scroll' : 'dm-fixed'
          return (
            <span key={a.key} className={a.mine ? `dm ${mode} is-mine` : `dm ${mode}`} style={style} onAnimationEnd={() => remove(a.key)}>
              {a.d.body}
            </span>
          )
        })}
    </div>
  )
}
