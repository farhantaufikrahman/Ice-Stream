// Klien HTTP kecil di atas fetch() untuk API gateway Ice-Stream.
//
// Cara login yang dipakai (sama dengan rancangan langkah 4b):
//   - access token (berlaku 15 menit) disimpan di MEMORI saja (variabel di
//     bawah), bukan localStorage, supaya script jahat (XSS) tidak bisa
//     mencurinya dari storage;
//   - refresh token ada di cookie httpOnly "ice_refresh" yang diatur server.
//     JavaScript tidak bisa membacanya; browser mengirimnya otomatis ke
//     /api/v1/auth/*.
// Saat halaman dibuka ulang, memori kosong, jadi aplikasi memanggil
// POST /auth/refresh untuk mendapat access token baru dari cookie.
//
// Perbandingan: di Node.js biasanya memakai axios + interceptor untuk retry
// 401; di Java (Spring) mirip ClientHttpRequestInterceptor; di Go mirip
// http.RoundTripper yang membungkus transport. Di sini cukup satu fungsi.

import type { AccessToken } from './types'

export const API_BASE = import.meta.env.VITE_API_BASE_URL || '/api/v1'

/** Error dari API, berbentuk {"error": {"code", "message"}} di backend. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

let accessToken: string | null = null
const sessionLostListeners = new Set<() => void>()

export function getAccessToken(): string | null {
  return accessToken
}

export function setAccessToken(token: string | null): void {
  accessToken = token
}

/**
 * Mendaftarkan fungsi yang dipanggil saat sesi berakhir (refresh token
 * ditolak). Mengembalikan fungsi untuk berhenti mendengarkan.
 */
export function onSessionLost(listener: () => void): () => void {
  sessionLostListeners.add(listener)
  return () => sessionLostListeners.delete(listener)
}

function sessionLost(): void {
  accessToken = null
  sessionLostListeners.forEach((listener) => listener())
}

// Hanya satu refresh yang boleh berjalan sekaligus. Server memutar
// (rotate) refresh token setiap kali dipakai, dan jika token LAMA dipakai
// lagi, server menganggapnya dicuri lalu mencabut semua sesi user. Jadi dua
// refresh paralel dengan cookie yang sama akan membuat user ter-logout.
let refreshInFlight: Promise<string | null> | null = null

/**
 * Meminta access token baru memakai cookie refresh token.
 * Mengembalikan null jika user belum login atau sesinya sudah berakhir.
 */
export function refreshAccessToken(): Promise<string | null> {
  refreshInFlight ??= refreshWithLock().finally(() => {
    refreshInFlight = null
  })
  return refreshInFlight
}

// Variabel refreshInFlight hanya berlaku di satu tab. Web Locks API
// menjaga hal yang sama antar-tab: tab kedua menunggu sampai tab pertama
// selesai, lalu memakai cookie yang sudah diperbarui.
// https://developer.mozilla.org/en-US/docs/Web/API/Web_Locks_API
async function refreshWithLock(): Promise<string | null> {
  if (typeof navigator !== 'undefined' && navigator.locks) {
    return navigator.locks.request('ice-stream-refresh', doRefresh)
  }
  return doRefresh()
}

async function doRefresh(): Promise<string | null> {
  const res = await fetch(`${API_BASE}/auth/refresh`, {
    method: 'POST',
    credentials: 'include',
  })
  if (res.status === 401) {
    accessToken = null
    return null
  }
  if (!res.ok) {
    throw await toApiError(res)
  }
  const body = (await res.json()) as AccessToken
  accessToken = body.access_token
  return accessToken
}

/**
 * - none: tanpa access token.
 * - optional: kirim token jika ada (misalnya GET /videos/:id, supaya
 *   viewer_liked ikut dijawab), tetap jalan jika belum login.
 * - required: wajib login; jika tidak ada sesi, melempar ApiError 401.
 */
export type AuthMode = 'none' | 'optional' | 'required'

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  /** Dikirim sebagai JSON. */
  body?: unknown
  auth?: AuthMode
  signal?: AbortSignal
  /** Kirim cookie. Hanya dibutuhkan endpoint /auth/*. */
  withCredentials?: boolean
  /** Request tetap terkirim walau tab ditutup (maksimal 64 KiB body). */
  keepalive?: boolean
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const auth = options.auth ?? 'none'

  if (auth === 'required' && !accessToken) {
    await refreshAccessToken()
    if (!accessToken) {
      throw new ApiError(401, 'UNAUTHENTICATED', 'silakan login dulu')
    }
  }

  const sentToken = auth !== 'none' ? accessToken : null
  let res = await send(path, options, sentToken)

  // Access token kedaluwarsa (atau server sudah me-restart kunci): minta
  // yang baru satu kali, lalu ulangi request yang sama.
  if (res.status === 401 && sentToken) {
    const fresh = await refreshAccessToken()
    if (fresh) {
      res = await send(path, options, fresh)
    } else if (auth === 'optional') {
      sessionLost()
      res = await send(path, options, null)
    } else {
      sessionLost()
    }
  }

  if (!res.ok) {
    throw await toApiError(res)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

function send(path: string, options: RequestOptions, token: string | null): Promise<Response> {
  const headers: Record<string, string> = {}
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  return fetch(`${API_BASE}${path}`, {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
    credentials: options.withCredentials ? 'include' : 'same-origin',
    keepalive: options.keepalive,
  })
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as { error?: { code?: string; message?: string } }
    if (body.error?.message) {
      return new ApiError(res.status, body.error.code ?? 'UNKNOWN', body.error.message)
    }
  } catch {
    // Bukan JSON (misalnya halaman 502 dari Gateway); pakai pesan umum.
  }
  return new ApiError(res.status, 'UNKNOWN', `request gagal (HTTP ${res.status})`)
}

/** Mengubah error apa pun menjadi kalimat yang boleh ditampilkan ke user. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError || (err instanceof Error && err.name === 'UploadError')) {
    // UploadError (src/api/upload.ts) juga sudah berisi kalimat untuk user.
    return err.message
  }
  if (err instanceof TypeError) {
    // fetch() melempar TypeError jika server tidak bisa dihubungi.
    return 'tidak bisa menghubungi server, periksa koneksi atau cluster'
  }
  return 'terjadi kesalahan, coba lagi'
}

/** true jika error berasal dari AbortController (request dibatalkan). */
export function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}
