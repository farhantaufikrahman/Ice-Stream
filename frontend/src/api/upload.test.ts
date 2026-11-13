import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UploadPartsResult, UploadTicket } from './types'
import { UploadError, putBlob, uploadMultipart, type PutBlobOptions } from './upload'

// File 25 byte, bagian 10 byte = 3 bagian (10, 10, 5).
const file = new Blob(['a'.repeat(25)])
const plan = { part_size: 10, part_count: 3 }
const future = new Date(Date.now() + 15 * 60_000).toISOString()

function fakeServer(stored: number[] = []) {
  const calls: number[][] = []
  const requestParts = vi.fn(async (_id: string, numbers: number[]): Promise<UploadPartsResult> => {
    calls.push(numbers)
    return {
      parts: numbers.map((n) => ({ part_number: n, url: `http://up.test/p${n}`, method: 'PUT', headers: {}, size: n === 3 ? 5 : 10 })),
      uploaded_part_numbers: stored,
      expires_at: future,
    }
  })
  return { requestParts, calls }
}

describe('uploadMultipart', () => {
  it('mengirim semua bagian dengan ukuran yang benar', async () => {
    const { requestParts, calls } = fakeServer()
    const sent: Array<[string, number]> = []
    const put = vi.fn(async (t: UploadTicket, blob: Blob, opts?: PutBlobOptions) => {
      sent.push([t.url, blob.size])
      opts?.onProgress?.(blob.size, blob.size)
    })
    const progress: number[] = []
    await uploadMultipart({ videoId: '42', file, plan, requestParts, put, concurrency: 1, onProgress: (l) => progress.push(l) })

    expect(sent).toEqual([
      ['http://up.test/p1', 10],
      ['http://up.test/p2', 10],
      ['http://up.test/p3', 5],
    ])
    // Pertanyaan awal (bagian tersimpan), lalu satu permintaan URL untuk semua bagian.
    expect(calls).toEqual([[], [1, 2, 3]])
    expect(progress.at(-1)).toBe(25)
  })

  it('melanjutkan: bagian yang sudah tersimpan tidak dikirim lagi', async () => {
    const { requestParts, calls } = fakeServer([1, 3])
    const put = vi.fn(async () => {})
    const progress: number[] = []
    await uploadMultipart({ videoId: '42', file, plan, requestParts, put, onProgress: (l) => progress.push(l) })

    expect(put).toHaveBeenCalledTimes(1)
    expect(calls).toEqual([[], [2]])
    expect(progress[0]).toBe(15)
    expect(progress.at(-1)).toBe(25)
  })

  it('koneksi putus: bagian dicoba ulang', async () => {
    const { requestParts } = fakeServer()
    let failures = 1
    const put = vi.fn(async () => {
      if (failures-- > 0) throw new UploadError(0, 'putus')
    })
    const sleep = vi.fn(async () => {})
    await uploadMultipart({ videoId: '42', file, plan, requestParts, put, sleep, concurrency: 1 })

    expect(put).toHaveBeenCalledTimes(4)
    expect(sleep).toHaveBeenCalledWith(1000)
  })

  it('URL kedaluwarsa (403): minta URL baru lalu kirim ulang', async () => {
    const { requestParts, calls } = fakeServer()
    let failures = 1
    const put = vi.fn(async () => {
      if (failures-- > 0) throw new UploadError(403, 'kedaluwarsa')
    })
    await uploadMultipart({ videoId: '42', file, plan, requestParts, put, sleep: async () => {}, concurrency: 1 })

    expect(calls).toEqual([[], [1, 2, 3], [1]])
  })

  it('menyerah setelah maxRetries, error lain langsung gagal', async () => {
    const { requestParts } = fakeServer()
    const put = vi.fn(async () => {
      throw new UploadError(0, 'putus')
    })
    await expect(
      uploadMultipart({ videoId: '42', file, plan, requestParts, put, sleep: async () => {}, maxRetries: 2, concurrency: 1 }),
    ).rejects.toThrow('putus')
    expect(put).toHaveBeenCalledTimes(3)

    const put400 = vi.fn(async () => {
      throw new UploadError(400, 'ditolak')
    })
    await expect(uploadMultipart({ videoId: '42', file, plan, requestParts, put: put400, concurrency: 1 })).rejects.toThrow('ditolak')
    expect(put400).toHaveBeenCalledTimes(1)
  })

  it('dibatalkan: berhenti dengan AbortError', async () => {
    const { requestParts } = fakeServer()
    const ctrl = new AbortController()
    const put = vi.fn(async () => {
      ctrl.abort()
    })
    await expect(uploadMultipart({ videoId: '42', file, plan, requestParts, put, signal: ctrl.signal, concurrency: 1 })).rejects.toMatchObject({
      name: 'AbortError',
    })
    expect(put).toHaveBeenCalledTimes(1)
  })
})

// XMLHttpRequest palsu: cukup untuk putBlob (open, setRequestHeader, send,
// abort, event load/error/abort dan upload "progress").
class FakeXHR {
  static last: FakeXHR
  status = 0
  method = ''
  url = ''
  headers: Record<string, string> = {}
  body: unknown
  private listeners: Record<string, Array<(e?: unknown) => void>> = {}
  private uploadListeners: Record<string, Array<(e: unknown) => void>> = {}
  upload = {
    addEventListener: (type: string, fn: (e: unknown) => void) => {
      ;(this.uploadListeners[type] ??= []).push(fn)
    },
  }
  constructor() {
    FakeXHR.last = this
  }
  addEventListener(type: string, fn: (e?: unknown) => void) {
    ;(this.listeners[type] ??= []).push(fn)
  }
  open(method: string, url: string) {
    this.method = method
    this.url = url
  }
  setRequestHeader(name: string, value: string) {
    this.headers[name] = value
  }
  send(body: unknown) {
    this.body = body
  }
  abort() {
    this.fire('abort')
  }
  progress(loaded: number, total: number) {
    for (const fn of this.uploadListeners.progress ?? []) fn({ loaded, total, lengthComputable: true })
  }
  finish(status: number) {
    this.status = status
    this.fire('load')
  }
  fire(type: string) {
    for (const fn of this.listeners[type] ?? []) fn()
  }
}

describe('putBlob', () => {
  const ticket: UploadTicket = { url: 'https://upload.test/videos/a?partNumber=1', method: 'PUT', headers: { 'Content-Type': 'video/mp4' } }
  const blob = new Blob(['a'.repeat(10)])

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('mengirim blob dengan header bertanda tangan dan melaporkan progres', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const progress: number[] = []
    const done = putBlob(ticket, blob, { onProgress: (l) => progress.push(l) })
    const xhr = FakeXHR.last
    expect(xhr.method).toBe('PUT')
    expect(xhr.headers).toEqual({ 'Content-Type': 'video/mp4' })
    expect(xhr.body).toBe(blob)
    xhr.progress(4, 10)
    xhr.finish(200)
    await done
    expect(progress).toEqual([4, 10])
  })

  it('status bukan 2xx dan koneksi putus menjadi UploadError', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const denied = putBlob(ticket, blob)
    FakeXHR.last.finish(403)
    await expect(denied).rejects.toMatchObject({ name: 'UploadError', status: 403 })

    const offline = putBlob(ticket, blob)
    FakeXHR.last.fire('error')
    await expect(offline).rejects.toMatchObject({ status: 0, message: expect.stringContaining('https://upload.test') })
  })

  it('dibatalkan lewat AbortSignal, termasuk yang sudah batal sebelum mulai', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    const ctrl = new AbortController()
    const sending = putBlob(ticket, blob, { signal: ctrl.signal })
    ctrl.abort()
    await expect(sending).rejects.toMatchObject({ name: 'AbortError' })

    await expect(putBlob(ticket, blob, { signal: ctrl.signal })).rejects.toMatchObject({ name: 'AbortError' })
  })
})
