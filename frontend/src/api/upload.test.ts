import { describe, expect, it, vi } from 'vitest'
import type { UploadPartsResult, UploadTicket } from './types'
import { UploadError, uploadMultipart, type PutBlobOptions } from './upload'

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
