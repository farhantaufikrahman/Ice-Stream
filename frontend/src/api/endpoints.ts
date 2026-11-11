// Satu fungsi untuk setiap endpoint REST yang dipakai frontend.
// Daftar lengkap route ada di backend/internal/httpserver/router.go.

import { API_BASE, apiRequest, setAccessToken } from './client'
import type {
  AccessToken,
  AppNotification,
  ChannelDetail,
  ChannelPage,
  ChannelSearchPage,
  ChannelStats,
  Comment,
  CommentPage,
  CreateUploadResult,
  Danmaku,
  DanmakuMode,
  DanmakuPage,
  HistoryPage,
  LikeResult,
  MarkReadResult,
  NotificationPage,
  Playlist,
  PlaylistPage,
  PlaylistVideoPage,
  PlaylistVideoResult,
  PlaylistVisibility,
  Profile,
  StatsDays,
  SubscriptionResult,
  Video,
  VideoDetail,
  VideoPage,
  ViewResult,
  WatchLaterPage,
  WatchLaterResult,
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

/** Tab Kreator di halaman pencarian. Dengan login, setiap kreator membawa viewer_subscribed. */
export function searchChannels(q: string, pageToken: string, signal?: AbortSignal, pageSize = PAGE_SIZE): Promise<ChannelSearchPage> {
  const params = new URLSearchParams({ q })
  return apiRequest<ChannelSearchPage>(`/search/channels?${params}&${pageQuery(pageToken, pageSize)}`, { auth: 'optional', signal })
}

/** Tab Playlist di halaman pencarian: hanya playlist publik yang berisi video. */
export function searchPlaylists(q: string, pageToken: string, signal?: AbortSignal): Promise<PlaylistPage> {
  const params = new URLSearchParams({ q })
  return apiRequest<PlaylistPage>(`/search/playlists?${params}&${pageQuery(pageToken)}`, { signal })
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

/** Statistik dasbor Creator Studio untuk 7, 28, atau 90 hari terakhir (WIB). */
export function getChannelStats(days: StatsDays, signal?: AbortSignal): Promise<ChannelStats> {
  return apiRequest<ChannelStats>(`/me/channel-stats?days=${days}`, { auth: 'required', signal })
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

// ---------- Komentar (langkah 17c) ----------

const COMMENT_PAGE_SIZE = 20

/** Komentar utama, terbaru lebih dulu. */
export function listComments(videoId: string, pageToken: string, signal?: AbortSignal): Promise<CommentPage> {
  const params = new URLSearchParams({ page_size: String(COMMENT_PAGE_SIZE) })
  if (pageToken) params.set('page_token', pageToken)
  return apiRequest<CommentPage>(`/videos/${encodeURIComponent(videoId)}/comments?${params}`, { signal })
}

/** Balasan untuk satu komentar utama, terlama lebih dulu. */
export function listReplies(videoId: string, commentId: string, pageToken: string, signal?: AbortSignal): Promise<CommentPage> {
  const params = new URLSearchParams({ page_size: String(COMMENT_PAGE_SIZE) })
  if (pageToken) params.set('page_token', pageToken)
  return apiRequest<CommentPage>(
    `/videos/${encodeURIComponent(videoId)}/comments/${encodeURIComponent(commentId)}/replies?${params}`,
    { signal },
  )
}

/** parentId diisi untuk membalas. Balasan untuk balasan ikut menempel ke komentar utamanya. */
export async function createComment(videoId: string, body: string, parentId?: string): Promise<Comment> {
  const res = await apiRequest<{ comment: Comment }>(`/videos/${encodeURIComponent(videoId)}/comments`, {
    method: 'POST',
    body: parentId ? { body, parent_id: parentId } : { body },
    auth: 'required',
  })
  return res.comment
}

/** Boleh oleh penulis komentar atau pemilik video. Komentar utama ikut menghapus balasannya. */
export async function deleteComment(videoId: string, commentId: string): Promise<void> {
  await apiRequest(`/videos/${encodeURIComponent(videoId)}/comments/${encodeURIComponent(commentId)}`, {
    method: 'DELETE',
    auth: 'required',
  })
}

// ---------- Danmaku (langkah 17c) ----------

/** Danmaku di rentang [fromMs, toMs]. Server membatasi satu rentang maksimal 6 menit. */
export function listDanmaku(
  videoId: string,
  fromMs: number,
  toMs: number,
  pageToken: string,
  signal?: AbortSignal,
): Promise<DanmakuPage> {
  const params = new URLSearchParams({ from_ms: String(fromMs), to_ms: String(toMs), page_size: '1000' })
  if (pageToken) params.set('page_token', pageToken)
  return apiRequest<DanmakuPage>(`/videos/${encodeURIComponent(videoId)}/danmaku?${params}`, { signal })
}

export interface NewDanmaku {
  body: string
  position_ms: number
  color: string
  mode: DanmakuMode
}

export async function sendDanmaku(videoId: string, input: NewDanmaku): Promise<Danmaku> {
  const res = await apiRequest<{ danmaku: Danmaku }>(`/videos/${encodeURIComponent(videoId)}/danmaku`, {
    method: 'POST',
    body: input,
    auth: 'required',
  })
  return res.danmaku
}

/** Alamat Server-Sent Events untuk danmaku live (dibuka dengan EventSource). */
export function danmakuLiveUrl(videoId: string): string {
  return `${API_BASE}/videos/${encodeURIComponent(videoId)}/danmaku/live`
}

// ---------- Kanal dan langganan (langkah 17d) ----------

/** Profil kanal + apakah penonton sudah mengikutinya (jika login). */
export function getChannel(username: string, signal?: AbortSignal): Promise<ChannelDetail> {
  return apiRequest<ChannelDetail>(`/users/${encodeURIComponent(username)}`, { auth: 'optional', signal })
}

/** Video READY milik kanal, terbaru dulu. */
export function listChannelVideos(username: string, pageToken: string, signal?: AbortSignal): Promise<VideoPage> {
  return apiRequest<VideoPage>(`/users/${encodeURIComponent(username)}/videos?${pageQuery(pageToken)}`, { signal })
}

/** Ikuti (true) atau berhenti mengikuti (false) sebuah kanal. Aman diulang. */
export function setSubscription(username: string, subscribed: boolean): Promise<SubscriptionResult> {
  return apiRequest<SubscriptionResult>(`/users/${encodeURIComponent(username)}/subscription`, {
    method: subscribed ? 'PUT' : 'DELETE',
    auth: 'required',
  })
}

/** Video terbaru dari semua kanal yang saya ikuti. */
export function listFeed(pageToken: string, signal?: AbortSignal): Promise<VideoPage> {
  return apiRequest<VideoPage>(`/me/feed?${pageQuery(pageToken)}`, { auth: 'required', signal })
}

/** Kanal yang saya ikuti, yang terakhir diikuti lebih dulu. */
export function listSubscriptions(pageToken: string, signal?: AbortSignal): Promise<ChannelPage> {
  return apiRequest<ChannelPage>(`/me/subscriptions?${pageQuery(pageToken)}`, { auth: 'required', signal })
}

// ---------- Pengaturan profil (langkah 17e) ----------

export interface ProfileChanges {
  display_name?: string
  bio?: string
}

/** PATCH /me/profile: kirim hanya field yang berubah. */
export async function updateMyProfile(changes: ProfileChanges): Promise<Profile> {
  const body = await apiRequest<{ user: Profile }>('/me/profile', { method: 'PATCH', body: changes, auth: 'required' })
  return body.user
}

/**
 * PUT /me/avatar: body = isi file gambar apa adanya (bukan multipart/form-data).
 * Server memotongnya persegi 256x256 dan menyimpannya ulang sebagai JPEG.
 */
export async function uploadAvatar(file: Blob): Promise<Profile> {
  const body = await apiRequest<{ user: Profile }>('/me/avatar', { method: 'PUT', body: file, auth: 'required' })
  return body.user
}

/** DELETE /me/avatar: kembali ke inisial berwarna. */
export async function removeAvatar(): Promise<Profile> {
  const body = await apiRequest<{ user: Profile }>('/me/avatar', { method: 'DELETE', auth: 'required' })
  return body.user
}

// ---------- Notifikasi dan riwayat tonton (langkah 17f) ----------

/** Jumlah notifikasi belum dibaca, 0 sampai 100 (server berhenti menghitung di 100). */
export async function getUnreadCount(signal?: AbortSignal): Promise<number> {
  const body = await apiRequest<{ unread_count: number }>('/me/notifications/unread-count', { auth: 'required', signal })
  return body.unread_count
}

/** Notifikasi terbaru lebih dulu. */
export function listNotifications(pageToken: string, signal?: AbortSignal): Promise<NotificationPage> {
  return apiRequest<NotificationPage>(`/me/notifications?${pageQuery(pageToken, LIST_PAGE_SIZE)}`, { auth: 'required', signal })
}

/** Tandai satu notifikasi sudah dibaca. Aman diulang. */
export function markNotificationRead(id: AppNotification['id']): Promise<MarkReadResult> {
  return apiRequest<MarkReadResult>(`/me/notifications/${encodeURIComponent(id)}/read`, { method: 'POST', auth: 'required' })
}

export function markAllNotificationsRead(): Promise<MarkReadResult> {
  return apiRequest<MarkReadResult>('/me/notifications/read-all', { method: 'POST', auth: 'required' })
}

/** Riwayat tonton: video yang terakhir ditonton lebih dulu. */
export function listHistory(pageToken: string, signal?: AbortSignal): Promise<HistoryPage> {
  return apiRequest<HistoryPage>(`/me/history?${pageQuery(pageToken, LIST_PAGE_SIZE)}`, { auth: 'required', signal })
}

/** Menghapus satu video dari riwayat (204, juga jika memang tidak ada). */
export async function deleteHistoryEntry(videoId: string): Promise<void> {
  await apiRequest(`/me/history/${encodeURIComponent(videoId)}`, { method: 'DELETE', auth: 'required' })
}

/** Menghapus seluruh riwayat, mengembalikan jumlah video yang dihapus. */
export async function clearHistory(): Promise<number> {
  const body = await apiRequest<{ deleted: number }>('/me/history', { method: 'DELETE', auth: 'required' })
  return body.deleted
}

// ---------- Tonton nanti (langkah 21b) ----------

/** Menyimpan (PUT) atau mengeluarkan (DELETE) video dari Tonton nanti. Aman diulang. */
export function setWatchLater(id: string, saved: boolean): Promise<WatchLaterResult> {
  return apiRequest<WatchLaterResult>(`/me/watch-later/${encodeURIComponent(id)}`, {
    method: saved ? 'PUT' : 'DELETE',
    auth: 'required',
  })
}

/** Tonton nanti: video yang terakhir disimpan lebih dulu. */
export function listWatchLater(pageToken: string, signal?: AbortSignal): Promise<WatchLaterPage> {
  return apiRequest<WatchLaterPage>(`/me/watch-later?${pageQuery(pageToken, LIST_PAGE_SIZE)}`, { auth: 'required', signal })
}

// ---------- Playlist (langkah 21c) ----------

export interface PlaylistInput {
  title: string
  description: string
  visibility: PlaylistVisibility
}

export async function createPlaylist(input: PlaylistInput): Promise<Playlist> {
  const body = await apiRequest<{ playlist: Playlist }>('/playlists', { method: 'POST', body: input, auth: 'required' })
  return body.playlist
}

/** Playlist PRIVATE hanya bisa dibuka pemiliknya; orang lain mendapat 404. */
export async function getPlaylist(id: string, signal?: AbortSignal): Promise<Playlist> {
  const body = await apiRequest<{ playlist: Playlist }>(`/playlists/${encodeURIComponent(id)}`, { auth: 'optional', signal })
  return body.playlist
}

/** Field yang tidak diisi (undefined) tidak ikut dikirim, jadi tidak diubah server. */
export async function updatePlaylist(id: string, changes: Partial<PlaylistInput>): Promise<Playlist> {
  const body = await apiRequest<{ playlist: Playlist }>(`/playlists/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: changes,
    auth: 'required',
  })
  return body.playlist
}

/** Menghapus playlist beserta isinya (videonya sendiri tidak terhapus). */
export async function deletePlaylist(id: string): Promise<void> {
  await apiRequest(`/playlists/${encodeURIComponent(id)}`, { method: 'DELETE', auth: 'required' })
}

/**
 * Isi playlist sesuai urutan ditambahkan. pageSize besar (maksimal 50)
 * dipakai panel playlist di halaman tonton, supaya lebih sedikit request.
 */
export function listPlaylistVideos(id: string, pageToken: string, signal?: AbortSignal, pageSize = LIST_PAGE_SIZE): Promise<PlaylistVideoPage> {
  return apiRequest<PlaylistVideoPage>(`/playlists/${encodeURIComponent(id)}/videos?${pageQuery(pageToken, pageSize)}`, {
    auth: 'optional',
    signal,
  })
}

/** Menambahkan (PUT) atau mengeluarkan (DELETE) video dari playlist. Aman diulang. */
export function setPlaylistVideo(playlistId: string, videoId: string, saved: boolean): Promise<PlaylistVideoResult> {
  return apiRequest<PlaylistVideoResult>(`/playlists/${encodeURIComponent(playlistId)}/videos/${encodeURIComponent(videoId)}`, {
    method: saved ? 'PUT' : 'DELETE',
    auth: 'required',
  })
}

/** Playlist saya (termasuk PRIVATE). Dengan videoId, tiap playlist membawa contains_video. */
export function listMyPlaylists(pageToken: string, videoId?: string, signal?: AbortSignal): Promise<PlaylistPage> {
  const params = new URLSearchParams(pageQuery(pageToken, MAX_PAGE_SIZE))
  if (videoId) params.set('video_id', videoId)
  return apiRequest<PlaylistPage>(`/me/playlists?${params}`, { auth: 'required', signal })
}

/** Playlist sebuah kanal. PRIVATE hanya ikut jika kanal itu milik penonton. */
export function listChannelPlaylists(username: string, pageToken: string, signal?: AbortSignal): Promise<PlaylistPage> {
  return apiRequest<PlaylistPage>(`/users/${encodeURIComponent(username)}/playlists?${pageQuery(pageToken)}`, {
    auth: 'optional',
    signal,
  })
}

const PAGE_SIZE = 24
/** Notifikasi dan riwayat: batas server 50, 20 per halaman cukup untuk satu layar. */
const LIST_PAGE_SIZE = 20
/** Batas page_size dari server. Dipakai dialog Simpan dan panel playlist supaya request lebih sedikit. */
export const MAX_PAGE_SIZE = 50

function pageQuery(pageToken: string, pageSize = PAGE_SIZE): string {
  const params = new URLSearchParams({ page_size: String(pageSize) })
  if (pageToken) {
    params.set('page_token', pageToken)
  }
  return params.toString()
}
