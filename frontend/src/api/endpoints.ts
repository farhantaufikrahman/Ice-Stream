// Satu fungsi untuk setiap endpoint REST yang dipakai frontend.
// Daftar lengkap route ada di backend/internal/httpserver/router.go.

import { apiRequest, setAccessToken } from './client'
import type {
  AccessToken,
  CreateUploadResult,
  LikeResult,
  Profile,
  Video,
  VideoDetail,
  VideoPage,
  ViewResult,
} from './types'

// ---------- Auth ----------

export async function login(loginName: string, password: string): Promise<void> {
  const body = await apiRequest<AccessToken>('/auth/login', {
    method: 'POST',
    body: { login: loginName, password },
    withCredentials: true,
  })
  setAccessToken(body.access_token)
}

export interface RegisterInput {
  username: string
  email: string
  password: string
  display_name: string
}

export async function register(input: RegisterInput): Promise<void> {
  await apiRequest('/auth/register', { method: 'POST', body: input })
}

export async function logout(): Promise<void> {
  try {
    await apiRequest('/auth/logout', { method: 'POST', withCredentials: true })
  } finally {
    // Walau server gagal dihubungi, tab ini tetap dianggap logout.
    setAccessToken(null)
  }
}

export async function getMyProfile(signal?: AbortSignal): Promise<Profile> {
  const body = await apiRequest<{ user: Profile }>('/me/profile', { auth: 'required', signal })
  return body.user
}

// ---------- Video ----------

export function listVideos(pageToken: string, signal?: AbortSignal): Promise<VideoPage> {
  return apiRequest<VideoPage>(`/videos?${pageQuery(pageToken)}`, { signal })
}

export function searchVideos(q: string, pageToken: string, signal?: AbortSignal): Promise<VideoPage> {
  const params = new URLSearchParams({ q })
  return apiRequest<VideoPage>(`/search/videos?${params}&${pageQuery(pageToken)}`, { signal })
}

export function getVideo(id: string, signal?: AbortSignal): Promise<VideoDetail> {
  return apiRequest<VideoDetail>(`/videos/${encodeURIComponent(id)}`, { auth: 'optional', signal })
}

export function recordView(id: string): Promise<ViewResult> {
  return apiRequest<ViewResult>(`/videos/${encodeURIComponent(id)}/views`, {
    method: 'POST',
    auth: 'optional',
  })
}

export function setLike(id: string, liked: boolean): Promise<LikeResult> {
  return apiRequest<LikeResult>(`/videos/${encodeURIComponent(id)}/like`, {
    method: liked ? 'PUT' : 'DELETE',
    auth: 'required',
  })
}

/** Menyimpan posisi tonton untuk fitur "lanjutkan menonton" (langkah 13). */
export function saveWatchProgress(id: string, positionMs: number, keepalive = false): Promise<unknown> {
  return apiRequest(`/me/history/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: { position_ms: Math.max(0, Math.floor(positionMs)) },
    auth: 'required',
    keepalive,
  })
}

// ---------- Unggah dan Studio (langkah 17b) ----------

export interface NewVideoInput {
  title: string
  description: string
  content_type: string
  size_bytes: number
}

/**
 * Langkah 1 unggah: mencatat video baru (status PENDING_UPLOAD) dan
 * meminta URL presigned untuk mengirim file langsung ke storage.
 */
export function createUpload(input: NewVideoInput): Promise<CreateUploadResult> {
  return apiRequest<CreateUploadResult>('/videos', { method: 'POST', body: input, auth: 'required' })
}

/** Langkah 3 unggah: memberi tahu server file sudah lengkap, lalu transcoding dimulai. */
export async function completeUpload(id: string): Promise<Video> {
  const body = await apiRequest<{ video: Video }>(`/videos/${encodeURIComponent(id)}/complete`, {
    method: 'POST',
    auth: 'required',
  })
  return body.video
}

/** Semua video milik user yang login, termasuk yang belum READY. */
export function listMyVideos(pageToken: string, signal?: AbortSignal): Promise<VideoPage> {
  return apiRequest<VideoPage>(`/me/videos?${pageQuery(pageToken)}`, { auth: 'required', signal })
}

/** Field yang tidak diisi (undefined) tidak ikut dikirim, jadi tidak diubah server. */
export async function updateVideo(id: string, changes: { title?: string; description?: string }): Promise<Video> {
  const body = await apiRequest<{ video: Video }>(`/videos/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: changes,
    auth: 'required',
  })
  return body.video
}

/** Server menjawab 409 jika video sedang menunggu atau menjalani transcoding. */
export async function deleteVideo(id: string): Promise<void> {
  await apiRequest(`/videos/${encodeURIComponent(id)}`, { method: 'DELETE', auth: 'required' })
}

const PAGE_SIZE = 24

function pageQuery(pageToken: string): string {
  const params = new URLSearchParams({ page_size: String(PAGE_SIZE) })
  if (pageToken) {
    params.set('page_token', pageToken)
  }
  return params.toString()
}
