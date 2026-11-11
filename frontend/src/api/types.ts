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
  /** Tipe file asli, misalnya "video/mp4". */
  content_type: string
  size_bytes: number
  created_at: string
  duration_ms?: number
  /** Kualitas hasil transcoding, misalnya ["720p", "480p"]. Ada saat READY. */
  renditions?: string[]
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
  /** Hanya untuk pemilik video: URL sementara untuk mengunduh file asli. */
  original?: { url: string; expires_at: string }
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

/**
 * Cara mengunggah file langsung ke storage (RustFS) memakai URL presigned:
 * kirim request `method` ke `url` dengan semua `headers`, body = file.
 */
export interface UploadTicket {
  url: string
  method: string
  headers: Record<string, string>
  expires_at: string
}

/** Jawaban POST /videos. */
export interface CreateUploadResult {
  video: Video
  upload: UploadTicket
}

// ---------- Komentar dan danmaku (langkah 17c) ----------

export interface Comment {
  id: string
  video_id: string
  /** Hanya ada pada balasan: id komentar utama yang dibalas. */
  parent_id?: string
  author_id: string
  /** Bisa kosong jika profil penulis gagal diambil server. */
  author?: Owner
  body: string
  /** Jumlah balasan (hanya bermakna pada komentar utama). */
  reply_count: number
  created_at: string
}

export interface CommentPage {
  comments: Comment[]
  next_page_token: string
}

export type DanmakuMode = 'SCROLL' | 'TOP' | 'BOTTOM'

export interface Danmaku {
  id: string
  video_id: string
  /** Detik ke berapa (dalam milidetik) danmaku muncul di video. */
  position_ms: number
  body: string
  /** Format "#RRGGBB". */
  color: string
  mode: DanmakuMode
  created_at: string
}

export interface DanmakuPage {
  danmaku: Danmaku[]
  next_page_token: string
}

// ---------- Kanal dan langganan (langkah 17d) ----------

/** Jawaban GET /users/:username (halaman kanal). */
export interface ChannelDetail {
  user: Profile
  /** Hanya ada jika penonton sudah login. */
  viewer_subscribed?: boolean
}

/** Jawaban PUT dan DELETE /users/:username/subscription. */
export interface SubscriptionResult {
  subscribed: boolean
  subscriber_count: number
}

/** Jawaban GET /me/subscriptions. */
export interface ChannelPage {
  channels: Profile[]
  next_page_token: string
}

// ---------- Notifikasi dan riwayat tonton (langkah 17f) ----------

export interface AppNotification {
  id: string
  /** Untuk sekarang selalu "NEW_VIDEO": kanal yang diikuti mengunggah video. */
  kind: string
  video: Video
  read: boolean
  read_at?: string
  created_at: string
}

export interface NotificationPage {
  notifications: AppNotification[]
  next_page_token: string
}

/** Jawaban POST .../read dan .../read-all. */
export interface MarkReadResult {
  updated: number
  unread_count: number
}

export interface HistoryEntry {
  video: Video
  /** Posisi terakhir yang ditonton. Dibagi video.duration_ms = panjang garis progres. */
  position_ms: number
  watched_at: string
}

export interface HistoryPage {
  entries: HistoryEntry[]
  next_page_token: string
}
