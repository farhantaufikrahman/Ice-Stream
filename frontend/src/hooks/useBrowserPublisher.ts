import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api/client'
import { createPublishToken } from '../api/endpoints'
import { bitrate, sampleStats, type SendSample } from '../lib/live'
import { publishWhip, WhipError, type WhipSession } from '../lib/whip'

/** Sumber siaran dari browser (desain GoLive.dc.html). */
export type BrowserSource = 'camera' | 'screen'

export type PublishState = 'idle' | 'starting' | 'live'

export interface SendStats {
  bitrate: number
  width: number
  height: number
  fps: number
  limitation: string
}

export interface BrowserPublisher {
  /** Gambar yang sedang disiapkan atau dikirim (untuk <video> pratinjau). */
  preview: MediaStream | null
  cameras: MediaDeviceInfo[]
  mics: MediaDeviceInfo[]
  /** Kamera atau mikrofon tidak bisa dibuka (izin ditolak, dipakai aplikasi lain, ...). */
  mediaError: string | null
  micOn: boolean
  camOn: boolean
  toggleMic: () => void
  toggleCam: () => void
  /** Level suara mikrofon 0 sampai 1, untuk indikator batang. */
  level: number
  state: PublishState
  /** Siaran gagal dimulai atau terputus. */
  error: string | null
  connection: RTCPeerConnectionState | 'idle'
  stats: SendStats | null
  start: () => Promise<void>
  stop: () => Promise<void>
}

/** Seberapa sering statistik kirim dibaca selama siaran. */
const STATS_INTERVAL_MS = 2000

/** Resolusi yang diminta dari kamera: 720p, sesuai desain. */
const CAMERA_VIDEO = { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } }

function mediaErrorMessage(err: unknown): string {
  if (err instanceof DOMException) {
    switch (err.name) {
      case 'NotAllowedError':
        return 'Izin kamera atau mikrofon ditolak. Izinkan lewat ikon gembok di kolom alamat browser, lalu muat ulang halaman.'
      case 'NotFoundError':
        return 'Kamera atau mikrofon tidak ditemukan.'
      case 'NotReadableError':
        return 'Kamera atau mikrofon sedang dipakai aplikasi lain.'
    }
  }
  return 'Kamera atau mikrofon tidak bisa dibuka.'
}

function stopTracks(stream: MediaStream | null) {
  stream?.getTracks().forEach((t) => t.stop())
}

/**
 * Semua urusan siaran dari browser (langkah 25b):
 *   - "camera": kamera + mikrofon langsung dibuka untuk pratinjau;
 *   - "screen": layar dipilih saat tombol Mulai siaran ditekan (browser
 *     hanya mengizinkan getDisplayMedia dari klik user), suaranya dari
 *     mikrofon;
 *   - start(): minta token ke API, lalu kirim lewat WHIP (lib/whip.ts).
 *
 * enabled=false (tab "Aplikasi (OBS)"): kamera tidak dibuka.
 *
 * getUserMedia: https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia
 * getDisplayMedia: https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getDisplayMedia
 */
export function useBrowserPublisher(source: BrowserSource, enabled: boolean, cameraId: string, micId: string): BrowserPublisher {
  const [preview, setPreview] = useState<MediaStream | null>(null)
  const [cameras, setCameras] = useState<MediaDeviceInfo[]>([])
  const [mics, setMics] = useState<MediaDeviceInfo[]>([])
  const [mediaError, setMediaError] = useState<string | null>(null)
  const [micOn, setMicOn] = useState(true)
  const [camOn, setCamOn] = useState(true)
  const [level, setLevel] = useState(0)
  const [state, setState] = useState<PublishState>('idle')
  const [error, setError] = useState<string | null>(null)
  const [connection, setConnection] = useState<RTCPeerConnectionState | 'idle'>('idle')
  const [stats, setStats] = useState<SendStats | null>(null)

  const sessionRef = useRef<WhipSession | null>(null)
  // Stream layar milik siaran yang sedang berjalan (dibuat saat start).
  const screenRef = useRef<MediaStream | null>(null)
  // Salinan state yang langsung berubah (tanpa menunggu render), supaya klik
  // ganda atau Akhiri saat masih menyambung terbaca dengan benar.
  const stateRef = useRef<PublishState>('idle')
  const setPhase = useCallback((p: PublishState) => {
    stateRef.current = p
    setState(p)
  }, [])

  const refreshDevices = useCallback(async () => {
    try {
      const list = await navigator.mediaDevices.enumerateDevices()
      setCameras(list.filter((d) => d.kind === 'videoinput' && d.deviceId))
      setMics(list.filter((d) => d.kind === 'audioinput' && d.deviceId))
    } catch {
      // Daftar perangkat hanya pelengkap pilihan.
    }
  }, [])

  // getUserMedia hanya ada di "secure context": https, atau http://localhost
  // dan http://*.localhost.
  const canCapture = typeof navigator !== 'undefined' && Boolean(navigator.mediaDevices?.getUserMedia)

  // Pratinjau kamera. Dibuka ulang saat kamera/mikrofon yang dipilih
  // berubah (pilihan dikunci selama siaran, lihat halaman Studio).
  useEffect(() => {
    if (!enabled || source !== 'camera' || !canCapture) return
    let cancelled = false
    let opened: MediaStream | null = null
    navigator.mediaDevices
      .getUserMedia({
        video: cameraId ? { ...CAMERA_VIDEO, deviceId: { exact: cameraId } } : CAMERA_VIDEO,
        audio: micId ? { deviceId: { exact: micId } } : true,
      })
      .then((stream) => {
        if (cancelled) {
          stopTracks(stream)
          return
        }
        opened = stream
        setMediaError(null)
        setPreview(stream)
        setMicOn(true)
        setCamOn(true)
        // Nama perangkat baru terbaca setelah izin diberikan.
        void refreshDevices()
      })
      .catch((err) => {
        if (!cancelled) setMediaError(mediaErrorMessage(err))
      })
    return () => {
      cancelled = true
      stopTracks(opened)
      setPreview((p) => (p === opened ? null : p))
    }
  }, [enabled, source, cameraId, micId, canCapture, refreshDevices])

  // Level suara: AnalyserNode membaca bentuk gelombang mikrofon, lalu
  // dihitung rata-rata kuadratnya (RMS). requestAnimationFrame berhenti
  // sendiri saat tab tidak terlihat.
  // https://developer.mozilla.org/en-US/docs/Web/API/AnalyserNode
  useEffect(() => {
    const track = preview?.getAudioTracks()[0]
    if (!track || typeof AudioContext === 'undefined') return
    const ctx = new AudioContext()
    const analyser = ctx.createAnalyser()
    analyser.fftSize = 512
    ctx.createMediaStreamSource(new MediaStream([track])).connect(analyser)
    const data = new Uint8Array(analyser.fftSize)
    let frame = 0
    let last = -1
    const tick = () => {
      analyser.getByteTimeDomainData(data)
      let sum = 0
      for (const v of data) sum += ((v - 128) / 128) ** 2
      // Dibulatkan ke 0,1 supaya state hanya berubah saat batangnya berubah.
      const next = Math.min(1, Math.round(Math.sqrt(sum / data.length) * 40) / 10)
      if (next !== last) {
        last = next
        setLevel(next)
      }
      frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(frame)
      void ctx.close()
      setLevel(0)
    }
  }, [preview])

  const stop = useCallback(async () => {
    const session = sessionRef.current
    sessionRef.current = null
    if (screenRef.current) {
      const screen = screenRef.current
      screenRef.current = null
      stopTracks(screen)
      setPreview((p) => (p === screen ? null : p))
    }
    setPhase('idle')
    setConnection('idle')
    setStats(null)
    await session?.stop()
  }, [setPhase])

  const start = useCallback(async () => {
    if (stateRef.current !== 'idle') return
    setError(null)
    setPhase('starting')
    let stream: MediaStream | null = null
    try {
      if (source === 'screen') {
        const display = await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: { ideal: 30 } }, audio: false })
        const tracks = [...display.getVideoTracks()]
        try {
          const mic = await navigator.mediaDevices.getUserMedia({ audio: micId ? { deviceId: { exact: micId } } : true })
          tracks.push(...mic.getAudioTracks())
        } catch {
          // Tanpa mikrofon: layar tetap disiarkan tanpa suara.
        }
        stream = new MediaStream(tracks)
        screenRef.current = stream
        setPreview(stream)
        setMicOn(true)
        setCamOn(true)
        // Tombol "Berhenti berbagi" bawaan browser mengakhiri siaran juga.
        display.getVideoTracks()[0]?.addEventListener('ended', () => void stop())
      } else {
        stream = preview
      }
      if (!stream || stream.getVideoTracks().length === 0) {
        throw new WhipError(0, 'Kamera belum siap.')
      }
      const token = await createPublishToken()
      const session = await publishWhip(token.whip_url, token.token, stream)
      // "as": TypeScript mengira nilainya masih 'starting', padahal stop()
      // bisa mengubahnya selama await di atas.
      if ((stateRef.current as PublishState) !== 'starting') {
        // Halaman ditutup atau Akhiri ditekan saat masih menyambung.
        await session.stop()
        return
      }
      sessionRef.current = session
      setConnection(session.pc.connectionState)
      session.pc.addEventListener('connectionstatechange', () => {
        setConnection(session.pc.connectionState)
        if (session.pc.connectionState === 'failed' && sessionRef.current === session) {
          setError('Koneksi siaran terputus. Tekan Mulai siaran untuk menyambung lagi.')
          void stop()
        }
      })
      setPhase('live')
    } catch (err) {
      const screen = screenRef.current
      if (screen) {
        screenRef.current = null
        stopTracks(screen)
        setPreview((p) => (p === screen ? null : p))
      }
      setPhase('idle')
      if (err instanceof DOMException && err.name === 'NotAllowedError' && source === 'screen') {
        // User menutup dialog pilih layar: bukan kesalahan.
        return
      }
      setError(err instanceof WhipError || err instanceof ApiError ? err.message : 'Siaran gagal dimulai.')
    }
  }, [source, micId, preview, stop, setPhase])

  // Statistik kirim selama siaran.
  useEffect(() => {
    if (state !== 'live') return
    let prev: SendSample | null = null
    const timer = setInterval(async () => {
      const pc = sessionRef.current?.pc
      if (!pc) return
      const cur = sampleStats((await pc.getStats()).values())
      setStats({ bitrate: bitrate(prev, cur), width: cur.width, height: cur.height, fps: cur.fps, limitation: cur.limitation })
      prev = cur
    }, STATS_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [state])

  // Halaman ditutup: siaran ikut berakhir.
  useEffect(() => () => void stop(), [stop])

  const toggleMic = () => {
    const next = !micOn
    preview?.getAudioTracks().forEach((t) => (t.enabled = next))
    setMicOn(next)
  }
  const toggleCam = () => {
    const next = !camOn
    preview?.getVideoTracks().forEach((t) => (t.enabled = next))
    setCamOn(next)
  }

  const shownMediaError = canCapture ? mediaError : 'Browser ini tidak bisa membuka kamera. Buka Ice-Stream lewat http://localhost atau https.'
  return { preview, cameras, mics, mediaError: shownMediaError, micOn, camOn, toggleMic, toggleCam, level, state, error, connection, stats, start, stop }
}
