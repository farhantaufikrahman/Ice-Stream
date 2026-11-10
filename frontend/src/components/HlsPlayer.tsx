import { useEffect, useLayoutEffect, useRef, useState } from 'react'

interface Props {
  /** URL master.m3u8 dari API (hls_url). */
  src: string
  poster?: string
  /** Mulai dari detik ke berapa (fitur "lanjutkan menonton"). */
  startAt?: number
  /** Dipanggil sekali, saat video pertama kali diputar. */
  onFirstPlay?: () => void
  /** Dipanggil berkala selama diputar, saat dijeda, dan saat player ditutup. */
  onProgress?: (seconds: number) => void
}

/** Seberapa sering posisi tonton dilaporkan selama video berjalan. */
const PROGRESS_INTERVAL_MS = 15_000

/**
 * Pemutar HLS. Chrome, Edge, dan Firefox tidak bisa memutar .m3u8 sendiri,
 * jadi hls.js mengambil potongan .ts lalu menyuapkannya ke <video> lewat
 * Media Source Extensions. Safari (terutama iPhone) bisa memutar HLS
 * langsung, jadi dipakai sebagai cadangan.
 * Dokumentasi: https://github.com/video-dev/hls.js/blob/master/docs/API.md
 * (lihat bagian tentang error dan recoverMediaError()).
 *
 * hls.js dimuat dengan import() dinamis supaya tidak memperberat halaman
 * yang tidak memutar video (beranda, login). Versi "light" dipakai karena
 * kita tidak butuh subtitle, multi-audio, atau DRM (ukurannya jauh lebih
 * kecil); H.264 + AAC hasil transcoder (langkah 6) tetap didukung.
 */
export function HlsPlayer({ src, poster, startAt = 0, onFirstPlay, onProgress }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState<string | null>(null)

  // Callback terbaru disimpan di ref supaya player tidak dibuat ulang setiap
  // kali komponen induk render.
  const callbacks = useRef({ onFirstPlay, onProgress })
  useLayoutEffect(() => {
    callbacks.current = { onFirstPlay, onProgress }
  })

  // startAt hanya dibaca saat src dimuat. Perubahan setelahnya (misalnya
  // data video dimuat ulang) tidak boleh melompatkan video yang sedang jalan.
  const startRef = useRef(startAt)
  useLayoutEffect(() => {
    startRef.current = startAt
  })

  // Laporan "mulai diputar" dan posisi tonton. Sengaja ditulis SEBELUM
  // effect hls.js: saat halaman ditutup React membersihkan effect sesuai
  // urutan, jadi posisi terakhir dibaca sebelum hls.destroy() mengosongkan
  // <video>.
  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    let played = false
    let timer: ReturnType<typeof setInterval> | undefined

    // Saat video habis, browser mengirim event pause lalu ended. Posisi yang
    // sama tidak perlu dilaporkan dua kali.
    let lastReported = -1
    const report = () => {
      const seconds = Math.floor(video.currentTime)
      if (!played || seconds === lastReported) return
      lastReported = seconds
      callbacks.current.onProgress?.(video.currentTime)
    }
    // Dicatat sendiri, karena video.paused tidak bisa dipercaya saat cleanup:
    // browser otomatis menjeda <video> yang sudah dilepas dari halaman, dan
    // React melepasnya SEBELUM menjalankan cleanup effect.
    let playing = false
    const onPlay = () => {
      playing = true
      if (!played) {
        played = true
        callbacks.current.onFirstPlay?.()
      }
      clearInterval(timer)
      timer = setInterval(report, PROGRESS_INTERVAL_MS)
    }
    const onStop = () => {
      playing = false
      clearInterval(timer)
      report()
    }

    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onStop)
    video.addEventListener('ended', onStop)
    return () => {
      video.removeEventListener('play', onPlay)
      video.removeEventListener('pause', onStop)
      video.removeEventListener('ended', onStop)
      clearInterval(timer)
      // Pindah halaman saat video masih berjalan: simpan posisi terakhir.
      if (playing) report()
    }
  }, [src])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    let cancelled = false
    let destroy = () => {}
    setError(null)

    const start = startRef.current
    const attach = async () => {
      const { default: Hls } = await import('hls.js/light')
      if (cancelled) return
      if (Hls.isSupported()) {
        const hls = new Hls({ startPosition: start > 0 ? start : -1 })
        let recoveredMedia = false
        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (!data.fatal) return
          // Error jaringan baru menjadi "fatal" setelah hls.js sendiri
          // mencoba ulang beberapa kali, jadi tidak perlu diulang lagi di sini
          // (dokumentasi hls.js memperingatkan bisa terjadi loop). Error media
          // (misalnya decode) dipulihkan sekali dengan recoverMediaError().
          if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !recoveredMedia) {
            recoveredMedia = true
            hls.recoverMediaError()
            return
          }
          setError(
            data.type === Hls.ErrorTypes.NETWORK_ERROR
              ? 'Video tidak bisa dimuat. Periksa koneksi lalu muat ulang halaman.'
              : 'Video tidak bisa diputar.',
          )
          hls.destroy()
        })
        hls.loadSource(src)
        hls.attachMedia(video)
        destroy = () => hls.destroy()
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        const seek = () => {
          if (start > 0) video.currentTime = start
        }
        video.addEventListener('loadedmetadata', seek, { once: true })
        video.src = src
        destroy = () => {
          video.removeEventListener('loadedmetadata', seek)
          video.removeAttribute('src')
          video.load()
        }
      } else {
        setError('Browser ini tidak bisa memutar video HLS.')
      }
    }
    attach().catch(() => {
      if (!cancelled) setError('Gagal memuat pemutar video.')
    })

    return () => {
      cancelled = true
      destroy()
    }
  }, [src])

  return (
    <div className="player">
      <video ref={videoRef} controls playsInline poster={poster} preload="metadata" />
      {error && <div className="player-error">{error}</div>}
    </div>
  )
}
