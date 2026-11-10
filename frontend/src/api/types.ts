// Bentuk JSON yang dikirim API gateway (backend/internal/httpserver).
// Semua id dikirim sebagai string karena int64 bisa melebihi angka aman
// JavaScript (Number.MAX_SAFE_INTEGER = 2^53 - 1).

export type VideoStatus = 'PENDING_UPLOAD' | 'UPLOADED' | 'PROCESSING' | 'READY' | 'FAILED'

export interface Owner {
  id: string
  username: string
  display_name: string
  /** "" jika belum ada foto profil. */
  avatar_url: string
}

export interface Video {
  id: string
  owner_id: string
  title: string
  description: string
  status: VideoStatus
  created_at: string
  duration_ms?: number
  hls_url?: string
  thumbnail_url?: string
  failure_reason?: string
  view_count: number
  comment_count: number
  like_count: number
  owner?: Owner
}

export interface VideoPage {
  videos: Video[]
  /** "" jika tidak ada halaman berikutnya. */
  next_page_token: string
}

export interface VideoDetail {
  video: Video
  /** Hanya ada jika penonton sudah login. */
  viewer_liked?: boolean
  /** Hanya ada jika penonton sudah login. 0 = belum pernah menonton. */
  resume_position_ms?: number
}

export interface Profile {
  id: string
  username: string
  display_name: string
  bio: string
  avatar_url: string
  created_at: string
  /** Hanya ada di profil milik sendiri (/me/profile). */
  email?: string
  subscriber_count: number
}

export interface AccessToken {
  access_token: string
  token_type: 'Bearer'
  expires_at: string
}

export interface LikeResult {
  liked: boolean
  like_count: number
}

export interface ViewResult {
  counted: boolean
  view_count: number
}
