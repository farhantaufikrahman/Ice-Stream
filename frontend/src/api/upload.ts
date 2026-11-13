// Mengirim file video langsung dari browser ke storage (RustFS) memakai
// URL presigned. File tidak lewat API gateway, jadi gateway tidak perlu
// menampung file berukuran gigabyte.
//
// Langkah 29: file dikirim per bagian (multipart upload, default 50 MB),
// karena Cloudflare paket gratis menolak request di atas 100 MB. Bonusnya,
// koneksi yang putus hanya mengulang satu bagian, dan "Coba lagi"
// melanjutkan dari bagian yang belum tersimpan.
//
// Kenapa XMLHttpRequest, bukan fetch()? fetch() belum punya event progres
// UNGGAH yang didukung semua browser, sedangkan XHR punya xhr.upload
// "progress" sejak lama. Dokumentasi:
// https://developer.mozilla.org/en-US/docs/Web/API/XMLHttpRequest/upload
//
// Perbandingan: di Node.js (server) biasanya memakai @aws-sdk/lib-storage
// Upload yang juga memotong file per bagian; di Java memakai
// S3TransferManager; di Go memakai feature/s3/manager.Uploader. Di sini
// browser yang memotong file dengan Blob.slice().

import { requestUploadParts } from './endpoints'
import type { UploadPartTicket, UploadPartsResult, UploadPlan, UploadTicket } from './types'

/** Unggahan ke storage gagal (bukan error dari API gateway). */
export class UploadError extends Error {
  /** Kode HTTP dari storage, 0 jika koneksi putus atau diblokir CORS. */
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'UploadError'
    this.status = status
  }
}

export interface PutBlobOptions {
  signal?: AbortSignal
  /** Dipanggil berkali-kali selama data terkirim. */
  onProgress?: (loaded: number, total: number) => void
}

/** Satu PUT ke URL presigned dengan body = blob. */
export function putBlob(ticket: UploadTicket, blob: Blob, { signal, onProgress }: PutBlobOptions = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(abortError())
      return
    }
    const xhr = new XMLHttpRequest()
    // Listener progres dipasang sebelum open()/send() (saran MDN).
    xhr.upload.addEventListener('progress', (e) => {
      onProgress?.(e.loaded, e.lengthComputable ? e.total : blob.size)
    })
    const onAbort = () => xhr.abort()
    const cleanup = () => signal?.removeEventListener('abort', onAbort)

    xhr.addEventListener('load', () => {
      cleanup()
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress?.(blob.size, blob.size)
        resolve()
        return
      }
      reject(new UploadError(xhr.status, storageMessage(xhr.status)))
    })
    xhr.addEventListener('error', () => {
      cleanup()
      // Browser tidak memberi detail (demi keamanan): bisa server mati,
      // koneksi putus, atau CORS ditolak. Detailnya ada di DevTools.
      reject(new UploadError(0, `tidak bisa menghubungi storage di ${originOf(ticket.url)}. Periksa koneksi internet, lalu coba lagi`))
    })
    xhr.addEventListener('abort', () => {
      cleanup()
      reject(abortError())
    })
    signal?.addEventListener('abort', onAbort, { once: true })

    xhr.open(ticket.method, ticket.url)
    // Header ini ikut ditandatangani di URL presigned, jadi wajib dikirim
    // persis sama. Content-Length diisi browser sendiri dari ukuran blob.
    for (const [name, value] of Object.entries(ticket.headers)) {
      xhr.setRequestHeader(name, value)
    }
    xhr.send(blob)
  })
}

export interface MultipartOptions {
  videoId: string
  file: Blob
  plan: UploadPlan
  signal?: AbortSignal
  /** loaded = byte yang sudah sampai di storage, termasuk sesi sebelumnya. */
  onProgress?: (loaded: number, total: number) => void
  /** Berapa bagian dikirim bersamaan. */
  concurrency?: number
  /** Berapa kali satu bagian dicoba ulang sebelum menyerah. */
  maxRetries?: number
  // Bisa diganti di tes.
  requestParts?: (id: string, partNumbers: number[], signal?: AbortSignal) => Promise<UploadPartsResult>
  put?: typeof putBlob
  sleep?: (ms: number) => Promise<void>
}

/** URL bagian dianggap kedaluwarsa 1 menit lebih awal (jam laptop bisa sedikit beda). */
const TICKET_MARGIN_MS = 60_000
/** Jumlah URL yang diminta sekaligus (server menerima maksimal 20). */
const TICKET_BATCH = 10

/**
 * Mengirim semua bagian yang belum tersimpan. Aman dipanggil ulang untuk
 * video yang sama: bagian yang sudah tersimpan (menurut server) dilewati.
 */
export async function uploadMultipart({
  videoId,
  file,
  plan,
  signal,
  onProgress,
  concurrency = 3,
  maxRetries = 3,
  requestParts = requestUploadParts,
  put = putBlob,
  sleep = delay,
}: MultipartOptions): Promise<void> {
  const total = file.size
  const sizeOf = (n: number) => Math.min(plan.part_size, total - (n - 1) * plan.part_size)

  const first = await requestParts(videoId, [], signal)
  const stored = new Set(first.uploaded_part_numbers)
  let doneBytes = 0
  for (const n of stored) doneBytes += sizeOf(n)
  const inflight = new Map<number, number>()
  const report = () => {
    let sending = 0
    for (const v of inflight.values()) sending += v
    onProgress?.(doneBytes + sending, total)
  }
  report()

  const queue: number[] = []
  for (let n = 1; n <= plan.part_count; n++) {
    if (!stored.has(n)) queue.push(n)
  }
  const tickets = new Map<number, { ticket: UploadPartTicket; expiresAt: number }>()
  const valid = (n: number) => {
    const t = tickets.get(n)
    return t !== undefined && t.expiresAt - TICKET_MARGIN_MS > Date.now()
  }

  const ticketFor = async (n: number): Promise<UploadPartTicket> => {
    if (!valid(n)) {
      // Minta URL untuk bagian ini sekaligus beberapa bagian berikutnya.
      const batch = [n, ...queue.filter((m) => !valid(m))].slice(0, TICKET_BATCH)
      const res = await requestParts(videoId, batch, signal)
      const expiresAt = Date.parse(res.expires_at)
      for (const p of res.parts) tickets.set(p.part_number, { ticket: p, expiresAt })
    }
    const t = tickets.get(n)
    if (!t) throw new UploadError(0, `server tidak memberi alamat untuk bagian ${n}`)
    return t.ticket
  }

  const sendPart = async (n: number) => {
    const start = (n - 1) * plan.part_size
    const blob = file.slice(start, start + sizeOf(n))
    for (let attempt = 0; ; attempt++) {
      if (signal?.aborted) throw abortError()
      try {
        const ticket = await ticketFor(n)
        await put(ticket, blob, {
          signal,
          onProgress: (loaded) => {
            inflight.set(n, loaded)
            report()
          },
        })
        inflight.delete(n)
        doneBytes += blob.size
        report()
        return
      } catch (err) {
        inflight.delete(n)
        report()
        if (!(err instanceof UploadError) || attempt >= maxRetries) throw err
        if (err.status === 403) {
          // URL kedaluwarsa (misalnya laptop tidur): minta yang baru.
          tickets.delete(n)
        } else if (err.status !== 0 && err.status < 500) {
          throw err
        }
        // Koneksi putus atau storage sibuk: tunggu 1, 2, 4 detik.
        await sleep(1000 * 2 ** attempt)
      }
    }
  }

  // Satu bagian gagal = worker lain berhenti mengambil bagian baru.
  let stopped = false
  const worker = async () => {
    for (let n = queue.shift(); n !== undefined && !stopped; n = queue.shift()) {
      try {
        await sendPart(n)
      } catch (err) {
        stopped = true
        throw err
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, Math.max(queue.length, 1)) }, worker))
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function abortError(): DOMException {
  // Sama dengan fetch() yang dibatalkan, supaya bisa dicek dengan isAbortError().
  return new DOMException('Unggahan dibatalkan', 'AbortError')
}

function originOf(url: string): string {
  try {
    return new URL(url).origin
  } catch {
    return url
  }
}

function storageMessage(status: number): string {
  if (status === 403) {
    // URL presigned berlaku 15 menit (UPLOAD_URL_TTL), atau header tidak cocok.
    return 'storage menolak unggahan (izin unggah kedaluwarsa), coba lagi'
  }
  return `storage menolak unggahan (HTTP ${status}), coba lagi`
}
