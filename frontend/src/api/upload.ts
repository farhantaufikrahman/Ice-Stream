// Mengirim file video langsung dari browser ke storage (RustFS) memakai
// URL presigned dari POST /videos. File tidak lewat API gateway, jadi
// gateway tidak perlu menampung file berukuran gigabyte.
//
// Kenapa XMLHttpRequest, bukan fetch()? fetch() belum punya event progres
// UNGGAH yang didukung semua browser, sedangkan XHR punya xhr.upload
// "progress" sejak lama. Dokumentasi:
// https://developer.mozilla.org/en-US/docs/Web/API/XMLHttpRequest/upload
//
// Perbandingan: di Node.js (server) biasanya memakai @aws-sdk/client-s3 +
// lib-storage Upload yang punya event "httpUploadProgress"; di Java memakai
// S3TransferManager; di Go memakai s3manager.Uploader. Di browser kita
// cukup satu PUT ke URL presigned.

import type { UploadTicket } from './types'

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

export interface PutFileOptions {
  signal?: AbortSignal
  /** Dipanggil berkali-kali selama file terkirim. */
  onProgress?: (loaded: number, total: number) => void
}

export function putFile(ticket: UploadTicket, file: Blob, { signal, onProgress }: PutFileOptions = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Unggahan dibatalkan', 'AbortError'))
      return
    }
    const xhr = new XMLHttpRequest()
    // Listener progres dipasang sebelum open()/send() (saran MDN).
    xhr.upload.addEventListener('progress', (e) => {
      onProgress?.(e.loaded, e.lengthComputable ? e.total : file.size)
    })
    const onAbort = () => xhr.abort()
    const cleanup = () => signal?.removeEventListener('abort', onAbort)

    xhr.addEventListener('load', () => {
      cleanup()
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress?.(file.size, file.size)
        resolve()
        return
      }
      reject(new UploadError(xhr.status, storageMessage(xhr.status)))
    })
    xhr.addEventListener('error', () => {
      cleanup()
      // Browser tidak memberi detail (demi keamanan): bisa server mati,
      // port belum dibuka, atau CORS ditolak. Detailnya ada di DevTools.
      reject(new UploadError(0, `tidak bisa menghubungi storage di ${originOf(ticket.url)}. Pastikan alamat itu bisa dibuka dari browser (lihat DevTools, tab Network)`))
    })
    xhr.addEventListener('abort', () => {
      cleanup()
      // Sama dengan fetch() yang dibatalkan, supaya bisa dicek dengan isAbortError().
      reject(new DOMException('Unggahan dibatalkan', 'AbortError'))
    })
    signal?.addEventListener('abort', onAbort, { once: true })

    xhr.open(ticket.method, ticket.url)
    // Header ini ikut ditandatangani di URL presigned (misalnya Content-Type),
    // jadi wajib dikirim persis sama. Content-Length diisi browser sendiri.
    for (const [name, value] of Object.entries(ticket.headers)) {
      xhr.setRequestHeader(name, value)
    }
    xhr.send(file)
  })
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
