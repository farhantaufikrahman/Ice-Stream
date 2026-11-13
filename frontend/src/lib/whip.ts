// Langkah 25b: siaran dari browser ke MediaMTX lewat WHIP.
//
// WHIP (WebRTC-HTTP Ingestion Protocol, RFC 9725) adalah cara standar
// memulai siaran WebRTC dengan satu request HTTP:
//   1. browser membuat RTCPeerConnection dan "offer" SDP (daftar codec dan
//      alamat jaringan yang bisa dipakai);
//   2. offer di-POST ke alamat WHIP (Content-Type: application/sdp);
//   3. server menjawab 201 + "answer" SDP + header Location (alamat sesi);
//   4. video dan suara lalu mengalir langsung lewat UDP/TCP (ICE);
//   5. DELETE ke Location untuk berhenti.
// Spesifikasi: https://www.rfc-editor.org/rfc/rfc9725
// MediaMTX: https://mediamtx.org/docs/usage/publish#webrtc
//
// Padanannya di backend: Go punya pion/webrtc, Java punya Kurento/Jitsi,
// Node.js biasanya memakai mediasoup. Di browser, WebRTC sudah bawaan.

/** Batas tunggu pengumpulan alamat jaringan (ICE) sebelum offer dikirim. */
const ICE_GATHER_TIMEOUT_MS = 2000

/** Error saat memulai siaran, dengan pesan yang bisa ditampilkan ke user. */
export class WhipError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'WhipError'
    this.status = status
  }
}

export interface WhipSession {
  pc: RTCPeerConnection
  /** Menutup koneksi dan memberi tahu server (DELETE ke alamat sesi). */
  stop: () => Promise<void>
}

/** Codec pendukung: bukan video, tapi ikut dikirim bersama H.264. */
const HELPER_CODECS = new Set(['video/rtx', 'video/red', 'video/ulpfec', 'video/flexfec-03'])

/**
 * Mengurutkan codec video supaya hanya H.264 yang ditawarkan.
 *
 * Kenapa? Penonton memutar siaran lewat HLS, dan HLS di MediaMTX hanya
 * menerima H.264/H.265/AV1/VP9. Jika browser mengirim VP8 (pilihan bawaan
 * Chrome), MediaMTX membuang videonya dan penonton hanya mendengar suara.
 * H.264 dipilih karena paling luas didukung, termasuk Safari di iPhone.
 *
 * packetization-mode=1 didahulukan (paket besar dipecah, kualitas lebih
 * baik). Mengembalikan null jika browser tidak bisa mengirim H.264.
 */
export function preferH264(codecs: readonly RTCRtpCodec[]): RTCRtpCodec[] | null {
  const h264 = codecs.filter((c) => c.mimeType.toLowerCase() === 'video/h264')
  if (h264.length === 0) return null
  const mode1 = (c: RTCRtpCodec) => /packetization-mode=1/.test(c.sdpFmtpLine ?? '')
  return [...h264.filter(mode1), ...h264.filter((c) => !mode1(c)), ...codecs.filter((c) => HELPER_CODECS.has(c.mimeType.toLowerCase()))]
}

/** Menunggu browser selesai mengumpulkan alamat jaringan, paling lama timeoutMs. */
function waitIceGathering(pc: RTCPeerConnection, timeoutMs: number): Promise<void> {
  if (pc.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer)
      pc.removeEventListener('icegatheringstatechange', check)
      resolve()
    }
    const check = () => {
      if (pc.iceGatheringState === 'complete') done()
    }
    // Alamat yang sudah terkumpul tetap dikirim; yang belum tidak ditunggu.
    const timer = setTimeout(done, timeoutMs)
    pc.addEventListener('icegatheringstatechange', check)
  })
}

/** Pesan untuk status HTTP dari MediaMTX. */
function whipErrorMessage(status: number): string {
  switch (status) {
    case 401:
    case 403:
      return 'Izin siaran ditolak. Muat ulang halaman lalu coba lagi.'
    case 400:
      return 'Server menolak siaran. Pastikan tidak ada siaran lain (misalnya OBS) yang sedang berjalan.'
    default:
      return `Server siaran tidak bisa dihubungi (HTTP ${status}).`
  }
}

/**
 * Memulai siaran: mengirim semua track di stream ke alamat WHIP. token
 * dikirim di header Authorization; MediaMTX meneruskannya ke VideoService
 * untuk diperiksa.
 *
 * Offer dikirim setelah semua alamat ICE terkumpul (tanpa "trickle ICE"),
 * jadi cukup satu POST. Di jaringan lokal ini hanya butuh beberapa
 * milidetik.
 */
export async function publishWhip(whipUrl: string, token: string, stream: MediaStream): Promise<WhipSession> {
  const pc = new RTCPeerConnection()
  try {
    for (const track of stream.getTracks()) {
      const transceiver = pc.addTransceiver(track, { direction: 'sendonly', streams: [stream] })
      if (track.kind === 'video') {
        const codecs = preferH264(RTCRtpSender.getCapabilities('video')?.codecs ?? [])
        if (!codecs) {
          throw new WhipError(0, 'Browser ini tidak bisa mengirim video H.264. Coba Chrome, Edge, atau Safari terbaru.')
        }
        transceiver.setCodecPreferences(codecs)
      }
    }
    await pc.setLocalDescription(await pc.createOffer())
    await waitIceGathering(pc, ICE_GATHER_TIMEOUT_MS)

    let res: Response
    try {
      res = await fetch(whipUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/sdp', Authorization: `Bearer ${token}` },
        body: pc.localDescription?.sdp ?? '',
      })
    } catch {
      throw new WhipError(0, 'Server siaran tidak bisa dihubungi. Periksa koneksi lalu coba lagi.')
    }
    if (res.status !== 201) {
      throw new WhipError(res.status, whipErrorMessage(res.status))
    }
    await pc.setRemoteDescription({ type: 'answer', sdp: await res.text() })

    // Location relatif terhadap alamat WHIP, misalnya "/live/7/whip/<uuid>".
    const location = res.headers.get('Location')
    const sessionUrl = location ? new URL(location, whipUrl).toString() : null
    let stopped = false
    return {
      pc,
      stop: async () => {
        if (stopped) return
        stopped = true
        pc.close()
        if (sessionUrl) {
          // Gagal tidak masalah: MediaMTX juga menutup sesi yang koneksinya putus.
          await fetch(sessionUrl, { method: 'DELETE', headers: { Authorization: `Bearer ${token}` } }).catch(() => {})
        }
      },
    }
  } catch (err) {
    pc.close()
    throw err
  }
}
