import { afterEach, describe, expect, it, vi } from 'vitest'
import { preferH264, publishWhip, WhipError } from './whip'

const codec = (mimeType: string, sdpFmtpLine?: string): RTCRtpCodec => ({ mimeType, clockRate: 90000, sdpFmtpLine })

describe('preferH264', () => {
  it('hanya H.264 (packetization-mode=1 dulu) dan codec pendukungnya', () => {
    const list = preferH264([
      codec('video/VP8'),
      codec('video/rtx'),
      codec('video/H264', 'packetization-mode=0;profile-level-id=42e01f'),
      codec('video/H264', 'packetization-mode=1;profile-level-id=42e01f'),
      codec('video/VP9', 'profile-id=0'),
      codec('video/red'),
    ])
    expect(list?.map((c) => `${c.mimeType} ${c.sdpFmtpLine ?? ''}`.trim())).toEqual([
      'video/H264 packetization-mode=1;profile-level-id=42e01f',
      'video/H264 packetization-mode=0;profile-level-id=42e01f',
      'video/rtx',
      'video/red',
    ])
  })

  it('null jika browser tidak bisa mengirim H.264', () => {
    expect(preferH264([codec('video/VP8'), codec('video/VP9')])).toBeNull()
  })
})

/** RTCPeerConnection palsu: cukup untuk alur WHIP tanpa jaringan. */
class FakePC {
  static last: FakePC
  iceGatheringState = 'complete'
  connectionState = 'new'
  localDescription: { sdp: string } | null = null
  remote: { type: string; sdp: string } | null = null
  closed = false
  transceivers: { kind: string; prefs: RTCRtpCodec[] | null }[] = []
  constructor() {
    FakePC.last = this
  }
  addTransceiver(track: { kind: string }) {
    const t = { kind: track.kind, prefs: null as RTCRtpCodec[] | null }
    this.transceivers.push(t)
    return { setCodecPreferences: (c: RTCRtpCodec[]) => (t.prefs = c) }
  }
  async createOffer() {
    return { type: 'offer', sdp: 'v=0 offer' }
  }
  async setLocalDescription(d: { sdp: string }) {
    this.localDescription = d
  }
  async setRemoteDescription(d: { type: string; sdp: string }) {
    this.remote = d
  }
  addEventListener() {}
  removeEventListener() {}
  close() {
    this.closed = true
  }
}

describe('publishWhip', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const stream = { getTracks: () => [{ kind: 'video' }, { kind: 'audio' }] } as unknown as MediaStream

  function stubWebRTC() {
    vi.stubGlobal('RTCPeerConnection', FakePC)
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => ({ codecs: [codec('video/VP8'), codec('video/H264', 'packetization-mode=1')] }) })
  }

  it('POST offer dengan token, pakai answer, lalu DELETE saat berhenti', async () => {
    stubWebRTC()
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValueOnce(new Response('v=0 answer', { status: 201, headers: { Location: '/live/7/whip/abc' } }))
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const session = await publishWhip('http://webrtc.example/live/7/whip', 'ipt_x', stream)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://webrtc.example/live/7/whip')
    expect(init?.method).toBe('POST')
    expect(init?.headers).toEqual({ 'Content-Type': 'application/sdp', Authorization: 'Bearer ipt_x' })
    expect(init?.body).toBe('v=0 offer')
    expect(FakePC.last.remote).toEqual({ type: 'answer', sdp: 'v=0 answer' })
    expect(FakePC.last.transceivers[0].prefs?.map((c) => c.mimeType)).toEqual(['video/H264'])

    await session.stop()
    expect(FakePC.last.closed).toBe(true)
    expect(fetchMock.mock.calls[1][0]).toBe('http://webrtc.example/live/7/whip/abc')
    expect(fetchMock.mock.calls[1][1]?.method).toBe('DELETE')
  })

  it('token ditolak: WhipError dan koneksi ditutup', async () => {
    stubWebRTC()
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValue(new Response('', { status: 401 })))
    const err = await publishWhip('http://webrtc.example/live/7/whip', 'ipt_salah', stream).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(WhipError)
    expect((err as WhipError).message).toMatch(/Izin siaran ditolak/)
    expect(FakePC.last.closed).toBe(true)
  })
})
