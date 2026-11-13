// Satu fungsi untuk setiap endpoint REST yang dipakai frontend.
// Daftar lengkap route ada di backend/internal/httpserver/router.go.

import { API_BASE, apiRequest, setAccessToken } from './client'
import type {
  AccessToken,
  AppNotification,
  BlockedVideoPage,
  BlockResult,
  ChannelDetail,
  ChannelPage,
  ChannelSearchPage,
  ChannelCommentPage,
  ChannelDanmakuPage,
  ChannelStats,
  Comment,
  CommentPage,
  CreateUploadResult,
  UploadPartsResult,
  Danmaku,
  DanmakuMode,
  DanmakuPage,
  HistoryPage,
  LikeResult,
  LiveChannel,
  LiveChannelChanges,
  LiveChatMessage,
  LiveStream,
  MarkReadResult,
  NotificationPage,
  OAuthProvider,
  Playlist,
  PlaylistPage,
  PlaylistVideoPage,
  PlaylistVideoResult,
  PlaylistVisibility,
  Profile,
  PublishToken,
  ReportedVideoPage,
  SearchCounts,
  StatsDays,
  StreamKeyResult,
  SubscriptionResult,
  TrendingList,
  Video,
  VideoDetail,
  VideoLanguage,
  VideoPage,
  VideoVisibility,
  VideoReport,
  ViewResult,
  WatchLaterPage,
  WatchLaterResult,
} from './types'
import type { ChannelSort, DurationFilter, SearchSort } from '../lib/discover'
import type { ReportReason } from '../lib/report'

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

/**
 * Meminta tautan "Lupa kata sandi?" (langkah 24c). Server selalu menjawab
 * 202, baik email itu terdaftar atau tidak.
 */
export async function requestPasswordReset(email: string): Promise<void> {
  await apiRequest('/auth/password/forgot', { method: 'POST', body: { email } })
}

/**
 * Mengganti kata sandi memakai token dari email. withCredentials supaya
 * server bisa menghapus cookie refresh lama di browser ini.
 */
export async function resetPassword(token: string, newPassword: string): Promise<void> {
  await apiRequest('/auth/password/reset', {
    method: 'POST',
    body: { token, new_password: newPassword },
    withCredentials: true,
  })
}

/**
 * Penyedia login yang dinyalakan server (langkah 26). Daftar kosong =
 * tombol Google/GitHub tidak ditampilkan.
 */
export async function listOAuthProviders(signal?: AbortSignal): Promise<OAuthProvider[]> {
  const body = await apiRequest<{ providers: OAuthProvider[] }>('/auth/oauth/providers', { signal })
  return body.providers
}

/**
 * Menukar kode sekali pakai dari halaman /oauth/selesai dengan sesi login
 * (cookie refresh token + access token), sama seperti login().
 */
export async function exchangeOAuthCode(code: string): Promise<void> {
  const body = await apiRequest<AccessToken>('/auth/oauth/exchange', {
    method: 'POST',
    body: { code },
    withCredentials: true,
  })
  setAccessToken(body.access_token)
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

/** Feed publik. category (misalnya "game") hanya mengambil video kategori itu. */
export function listVideos(pageToken: string, signal?: AbortSignal, category?: string): Promise<VideoPage> {
  const filter = category ? `${new URLSearchParams({ category })}&` : ''
  return apiRequest<VideoPage>(`/videos?${filter}${pageQuery(pageToken)}`, { signal })
}

/** Pencarian video. sort dan duration hanya dikirim jika bukan bawaan (langkah 24a). */
export function searchVideos(
  q: string,
  pageToken: string,
  signal?: AbortSignal,
  sort: SearchSort = 'relevance',
  duration: DurationFilter = '',
): Promise<VideoPage> {
  const params = new URLSearchParams({ q })
  if (sort !== 'relevance') params.set('sort', sort)
  if (duration) params.set('duration', duration)
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

/**
 * Angka di setiap tab pencarian (langkah 27). duration hanya berlaku untuk
 * tab Video, sama dengan searchVideos.
 */
export function getSearchCounts(q: string, duration: DurationFilter = '', signal?: AbortSignal): Promise<SearchCounts> {
  const params = new URLSearchParams({ q })
  if (duration) params.set('duration', duration)
  return apiRequest<SearchCounts>(`/search/counts?${params}`, { signal })
}

/**
 * "Rekomendasi untukmu" di Beranda (langkah 27). seed mengubah susunan
 * acaknya: seed yang sama memberi urutan yang sama, jadi tombol "Muat
 * ulang" cukup mengganti seed. Dengan login, rekomendasi memakai riwayat
 * tonton dan kanal yang diikuti.
 */
export async function listRecommended(seed: string, category: string, limit: number, signal?: AbortSignal): Promise<Video[]> {
  const params = new URLSearchParams({ seed, limit: String(limit) })
  if (category) params.set('category', category)
  const res = await apiRequest<VideoPage>(`/videos/recommended?${params}`, { auth: 'optional', signal })
  return res.videos
}

/** Memilih "Video unggulan" kanal sendiri. null = hapus pilihan. */
export async function setFeaturedVideo(videoId: string | null): Promise<Video | null> {
  const res = await apiRequest<{ featured_video: Video | null }>('/me/featured-video', {
    method: 'PUT',
    body: { video_id: videoId },
    auth: 'required',
  })
  return res.featured_video
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
  /** Boleh tidak dikirim (tanpa kategori). */
  category?: string
  tags?: string[]
  /** Langkah 26. Tidak dikirim = PUBLIC, bukan draf, tanpa bahasa. */
  visibility?: VideoVisibility
  draft?: boolean
  language?: VideoLanguage
}

/**
 * PATCH /videos/:id. Field yang tidak dikirim tidak diubah. category ""
 * menghapus kategori, tags [] menghapus semua tag.
 */
export interface VideoChanges {
  title?: string
  description?: string
  category?: string
  tags?: string[]
  /** Langkah 26. draft: false = "Terbitkan". */
  visibility?: VideoVisibility
  draft?: boolean
  language?: VideoLanguage
}

/**
 * Langkah 1 unggah: mencatat video baru (status PENDING_UPLOAD) dan
 * memulai multipart upload di storage (langkah 29).
 */
export function createUpload(input: NewVideoInput): Promise<CreateUploadResult> {
  return apiRequest<CreateUploadResult>('/videos', { method: 'POST', body: input, auth: 'required' })
}

/**
 * Langkah 2 unggah: URL presigned untuk bagian-bagian file (maksimal 20
 * sekaligus), dan daftar bagian yang sudah tersimpan. `partNumbers` kosong
 * = hanya menanyakan bagian yang sudah tersimpan (melanjutkan upload).
 */
export function requestUploadParts(id: string, partNumbers: number[], signal?: AbortSignal): Promise<UploadPartsResult> {
  return apiRequest<UploadPartsResult>(`/videos/${encodeURIComponent(id)}/upload-parts`, {
    method: 'POST',
    body: { part_numbers: partNumbers },
    auth: 'required',
    signal,
  })
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

/**
 * Statistik Creator Studio untuk 7, 28, atau 90 hari terakhir (WIB).
 * videoId (halaman Analitik, langkah 26) membatasi angka ke satu video.
 */
export function getChannelStats(days: StatsDays, signal?: AbortSignal, videoId?: string): Promise<ChannelStats> {
  const params = new URLSearchParams({ days: String(days) })
  if (videoId) params.set('video_id', videoId)
  return apiRequest<ChannelStats>(`/me/channel-stats?${params}`, { auth: 'required', signal })
}

/**
 * PUT /videos/:id/thumbnail (langkah 26): body = isi file gambar apa
 * adanya, sama seperti uploadAvatar. Server memotongnya 16:9.
 */
export async function uploadThumbnail(id: string, file: Blob): Promise<Video> {
  const body = await apiRequest<{ video: Video }>(`/videos/${encodeURIComponent(id)}/thumbnail`, {
    method: 'PUT',
    body: file,
    auth: 'required',
  })
  return body.video
}

/** PUT /videos/:id/thumbnail/frame: pakai salah satu video.thumbnail_frames. */
export async function chooseThumbnailFrame(id: string, positionMs: number): Promise<Video> {
  const body = await apiRequest<{ video: Video }>(`/videos/${encodeURIComponent(id)}/thumbnail/frame`, {
    method: 'PUT',
    body: { position_ms: positionMs },
    auth: 'required',
  })
  return body.video
}

// ---------- Komentar dan danmaku di Creator Studio (langkah 26) ----------

function studioQuery(videoId: string, pageToken: string, pageSize: number): string {
  const params = new URLSearchParams({ page_size: String(pageSize) })
  if (videoId) params.set('video_id', videoId)
  if (pageToken) params.set('page_token', pageToken)
  return params.toString()
}

/** Komentar dan balasan di video milik user, terbaru lebih dulu. videoId "" = semua video. */
export function listMyComments(videoId: string, pageToken: string, signal?: AbortSignal): Promise<ChannelCommentPage> {
  return apiRequest<ChannelCommentPage>(`/me/comments?${studioQuery(videoId, pageToken, 20)}`, { auth: 'required', signal })
}

/** Danmaku di video milik user, terbaru lebih dulu. videoId "" = semua video. */
export function listMyDanmaku(videoId: string, pageToken: string, signal?: AbortSignal): Promise<ChannelDanmakuPage> {
  return apiRequest<ChannelDanmakuPage>(`/me/danmaku?${studioQuery(videoId, pageToken, 50)}`, { auth: 'required', signal })
}

/** Pengirim atau pemilik video boleh menghapus danmaku. */
export async function deleteDanmaku(id: string): Promise<void> {
  await apiRequest(`/me/danmaku/${encodeURIComponent(id)}`, { method: 'DELETE', auth: 'required' })
}

/** Field yang tidak diisi (undefined) tidak ikut dikirim, jadi tidak diubah server. */
export async function updateVideo(id: string, changes: VideoChanges): Promise<Video> {
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

/** Video READY milik kanal: terbaru dulu, atau tayangan terbanyak dulu (sort "popular", langkah 24a). */
export function listChannelVideos(username: string, pageToken: string, signal?: AbortSignal, sort: ChannelSort = 'newest'): Promise<VideoPage> {
  const filter = sort === 'popular' ? 'sort=popular&' : ''
  return apiRequest<VideoPage>(`/users/${encodeURIComponent(username)}/videos?${filter}${pageQuery(pageToken)}`, { signal })
}

/** Trending: tayangan 7 hari terakhir terbanyak. category "" = semua kategori. limit maksimal 50. */
export function listTrending(category: string, limit: number, signal?: AbortSignal): Promise<TrendingList> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (category) params.set('category', category)
  return apiRequest<TrendingList>(`/trending/videos?${params}`, { signal })
}

/** Laporkan video (langkah 24b). Mengirim lagi mengganti laporan sebelumnya. */
export function reportVideo(id: string, reason: ReportReason, details: string): Promise<{ reported_at: string }> {
  return apiRequest<{ reported_at: string }>(`/videos/${encodeURIComponent(id)}/reports`, {
    method: 'POST',
    body: { reason, details },
    auth: 'required',
  })
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

// ---------- Siaran langsung (langkah 25) ----------

export interface LiveFilter {
  /** 1 sampai 50, default server 20. */
  limit?: number
  category?: string
  /** Cari di judul (tab Live di halaman pencarian). */
  q?: string
}

/** Siaran yang sedang LIVE, terbaru dulu. Tanpa login. */
export async function listLive(filter: LiveFilter = {}, signal?: AbortSignal): Promise<LiveStream[]> {
  const params = new URLSearchParams()
  if (filter.limit) params.set('limit', String(filter.limit))
  if (filter.category) params.set('category', filter.category)
  if (filter.q) params.set('q', filter.q)
  const res = await apiRequest<{ streams: LiveStream[] }>(`/live?${params}`, { signal })
  return res.streams
}

/** Satu siaran (LIVE atau ENDED). */
export async function getLive(id: string, signal?: AbortSignal): Promise<LiveStream> {
  const res = await apiRequest<{ stream: LiveStream }>(`/live/${encodeURIComponent(id)}`, { signal })
  return res.stream
}

/** Pengaturan siaran milik sendiri, dibuat otomatis saat pertama kali dibuka. */
export async function getMyLive(signal?: AbortSignal): Promise<LiveChannel> {
  const res = await apiRequest<{ channel: LiveChannel }>('/me/live', { auth: 'required', signal })
  return res.channel
}

/**
 * Mengubah judul, kategori (juga siaran yang sedang berjalan), dan pilihan
 * siaran. Pilihan yang tidak diisi tidak diubah.
 */
export async function updateMyLive(changes: LiveChannelChanges): Promise<LiveChannel> {
  const res = await apiRequest<{ channel: LiveChannel }>('/me/live', {
    method: 'PATCH',
    body: changes,
    auth: 'required',
  })
  return res.channel
}

/** Membuat stream key baru untuk OBS. Key lama langsung tidak berlaku. */
export function resetStreamKey(): Promise<StreamKeyResult> {
  return apiRequest<StreamKeyResult>('/me/live/stream-key', { method: 'POST', auth: 'required' })
}

/** Token pendek untuk siaran dari browser (WebRTC lewat WHIP). */
export function createPublishToken(): Promise<PublishToken> {
  return apiRequest<PublishToken>('/me/live/publish-token', { method: 'POST', auth: 'required' })
}

/** Pesan chat terakhir sebuah siaran, urut dari yang paling lama. */
export async function listLiveChat(streamId: string, signal?: AbortSignal): Promise<LiveChatMessage[]> {
  const res = await apiRequest<{ messages: LiveChatMessage[] }>(`/live/${encodeURIComponent(streamId)}/chat`, { signal })
  return res.messages
}

/**
 * Mengirim pesan chat. Mode lambat: 429 jika kurang dari 3 detik dari pesan
 * sebelumnya. Chat khusus pengikut: 403 jika belum mengikuti kanal.
 */
export async function sendLiveChat(streamId: string, body: string, color: string): Promise<LiveChatMessage> {
  const res = await apiRequest<{ message: LiveChatMessage }>(`/live/${encodeURIComponent(streamId)}/chat`, {
    method: 'POST',
    body: { body, color },
    auth: 'required',
  })
  return res.message
}

/** Alamat Server-Sent Events chat baru sebuah siaran (untuk EventSource). */
export function liveChatUrl(streamId: string): string {
  return `${API_BASE}/live/${encodeURIComponent(streamId)}/chat/live`
}

// ---------- Moderasi admin (langkah 28) ----------
// Semua endpoint /admin menjawab 403 jika user bukan admin.

/** Antrean video yang punya laporan terbuka, laporan terbanyak dulu. */
export function listReportedVideos(pageToken: string, signal?: AbortSignal): Promise<ReportedVideoPage> {
  return apiRequest<ReportedVideoPage>(`/admin/reports?${pageQuery(pageToken, LIST_PAGE_SIZE)}`, { auth: 'required', signal })
}

/** Laporan terbuka untuk satu video, terbaru dulu. */
export async function listVideoReports(videoId: string, signal?: AbortSignal): Promise<VideoReport[]> {
  const res = await apiRequest<{ reports: VideoReport[] }>(`/admin/reports/${encodeURIComponent(videoId)}`, { auth: 'required', signal })
  return res.reports
}

/** Menurunkan video. note wajib untuk alasan "other", maksimal 500 karakter. */
export function blockVideo(videoId: string, reason: ReportReason, note: string): Promise<BlockResult> {
  return apiRequest<BlockResult>(`/admin/videos/${encodeURIComponent(videoId)}/block`, {
    method: 'POST',
    body: { reason, note },
    auth: 'required',
  })
}

/** Menolak semua laporan terbuka sebuah video (videonya tetap tayang). */
export function dismissReports(videoId: string): Promise<{ closed_reports: number }> {
  return apiRequest<{ closed_reports: number }>(`/admin/videos/${encodeURIComponent(videoId)}/dismiss`, {
    method: 'POST',
    auth: 'required',
  })
}

/** Memulihkan video ke visibilitas sebelum diturunkan. */
export async function unblockVideo(videoId: string): Promise<Video> {
  const res = await apiRequest<{ video: Video }>(`/admin/videos/${encodeURIComponent(videoId)}/unblock`, {
    method: 'POST',
    auth: 'required',
  })
  return res.video
}

/** Video yang sedang diturunkan, terbaru dulu. */
export function listBlockedVideos(pageToken: string, signal?: AbortSignal): Promise<BlockedVideoPage> {
  return apiRequest<BlockedVideoPage>(`/admin/blocked?${pageQuery(pageToken, LIST_PAGE_SIZE)}`, { auth: 'required', signal })
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
