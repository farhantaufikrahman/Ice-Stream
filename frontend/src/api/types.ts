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
  /** Langkah 24a: jumlah danmaku yang pernah dikirim ke video ini. */
  danmaku_count: number
  /** Langkah 23. "" jika video tidak punya kategori. */
  category: string
  /** Tanpa "#", misalnya ["esKrim", "anakKos"]. Bisa kosong. */
  tags: string[]
  owner?: Owner
}

export interface VideoPage {
  videos: Video[]
  /** "" jika tidak ada halaman berikutnya. */
  next_page_token: string
}

/** Satu video Trending (GET /trending/videos). */
export interface TrendingVideo extends Video {
  /** Tayangan 7 hari terakhir (termasuk hari ini, tanggal WIB): angka urutannya. */
  recent_views: number
}

export interface TrendingList {
  videos: TrendingVideo[]
}

export interface VideoDetail {
  video: Video
  /** Hanya untuk pemilik video: URL sementara untuk mengunduh file asli. */
  original?: { url: string; expires_at: string }
  /** Hanya ada jika penonton sudah login. */
  viewer_liked?: boolean
  /** Hanya ada jika penonton sudah login. 0 = belum pernah menonton. */
  resume_position_ms?: number
  /** Hanya ada jika penonton sudah login: true jika video ada di Tonton nanti. */
  viewer_saved?: boolean
  /** Hanya ada jika penonton sudah login: true jika video ada di salah satu playlist miliknya. */
  viewer_in_playlist?: boolean
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

// ---------- Tonton nanti (langkah 21b) ----------

export interface WatchLaterEntry {
  video: Video
  /** Waktu video disimpan. */
  added_at: string
}

export interface WatchLaterPage {
  entries: WatchLaterEntry[]
  next_page_token: string
}

/** Jawaban PUT dan DELETE /me/watch-later/:id. */
export interface WatchLaterResult {
  saved: boolean
  /** Hanya ada jika saved = true. */
  added_at?: string
}

// ---------- Playlist (langkah 21c) ----------

export type PlaylistVisibility = 'PUBLIC' | 'PRIVATE'

export interface Playlist {
  id: string
  owner_id: string
  title: string
  description: string
  visibility: PlaylistVisibility
  /** Jumlah video yang bisa diputar (READY). */
  video_count: number
  /** Thumbnail video pertama, "" jika playlist masih kosong. */
  thumbnail_url: string
  created_at: string
  updated_at: string
  /** Hanya ada di GET /me/playlists?video_id=...: true jika video itu ada di playlist ini. */
  contains_video?: boolean
  /** Hanya ada di GET /playlists/:id. */
  owner?: Owner
}

export interface PlaylistPage {
  playlists: Playlist[]
  next_page_token: string
}

export interface PlaylistEntry {
  video: Video
  /** Waktu video ditambahkan ke playlist. */
  added_at: string
}

export interface PlaylistVideoPage {
  entries: PlaylistEntry[]
  next_page_token: string
}

/** Jawaban PUT dan DELETE /playlists/:id/videos/:videoId (bentuknya sama dengan Tonton nanti). */
export type PlaylistVideoResult = WatchLaterResult

/** Pilihan rentang di dasbor Creator Studio (desain Studio.dc.html). */
export type StatsDays = 7 | 28 | 90

export interface DailyViews {
  /** Tanggal WIB, misalnya "2026-11-11". */
  date: string
  views: number
}

export interface ChannelTotals {
  views: number
  danmaku: number
  comments: number
  /** null jika jumlahnya sedang tidak bisa dimuat. */
  new_subscribers: number | null
}

/** Jawaban GET /me/channel-stats?days=... */
export interface ChannelStats {
  days: StatsDays
  /** Tepat sebanyak days, dari yang terlama sampai hari ini. */
  daily: DailyViews[]
  current: ChannelTotals
  /** Periode sebelumnya dengan panjang yang sama, untuk persentase naik/turun. */
  previous: ChannelTotals
}

/** Satu kreator di hasil pencarian (GET /search/channels). */
export interface ChannelResult extends Profile {
  /** Hanya ada jika sudah login: true jika sudah mengikuti kreator ini. */
  viewer_subscribed?: boolean
}

export interface ChannelSearchPage {
  channels: ChannelResult[]
  next_page_token: string
}

// ---------- Siaran langsung (langkah 25) ----------

export type LiveStatus = 'LIVE' | 'ENDED'

/** Satu kali siaran (GET /live, GET /live/:id). */
export interface LiveStream {
  id: string
  owner_id: string
  /** Kosong jika UserService sedang gagal. */
  owner?: Owner
  title: string
  /** "" = tanpa kategori. */
  category: string
  status: LiveStatus
  started_at: string
  /** Hanya ada setelah siaran selesai. */
  ended_at?: string
  /** Alamat HLS untuk hls.js. Hanya ada selama LIVE. */
  playback_url?: string
  viewer_count: number
  peak_viewers: number
}

/** Pengaturan siaran milik sendiri (GET /me/live). */
export interface LiveChannel {
  title: string
  category: string
  /** Isi kolom "Server" di OBS, misalnya rtmp://localhost:1935/live. */
  ingest_url: string
  has_stream_key: boolean
  stream_key_created_at?: string
  /** Siaran yang sedang berjalan, null jika tidak sedang live. */
  current: LiveStream | null
}

/** Jawaban POST /me/live/stream-key. stream_key hanya muncul sekali. */
export interface StreamKeyResult {
  channel: LiveChannel
  stream_key: string
}

/** Jawaban POST /me/live/publish-token (siaran dari browser). */
export interface PublishToken {
  whip_url: string
  token: string
  expires_at: string
}

/** Satu pesan chat siaran (GET /live/:id/chat, event SSE "chat"). */
export interface LiveChatMessage {
  id: string
  stream_id: string
  /** null jika profil pengirim sedang tidak bisa dimuat. */
  user: Owner | null
  body: string
  /** Format "#RRGGBB". */
  color: string
  created_at: string
}
