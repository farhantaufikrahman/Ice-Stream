package livestream

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/textrule"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/video"
)

// Repository adalah semua akses database yang dibutuhkan Service.
// Implementasi Oracle ada di oracle_repository.go; test memakai versi palsu.
type Repository interface {
	// GetOrCreateChannel mengambil pengaturan siaran milik user, atau
	// membuatnya dengan judul DefaultTitle jika belum ada.
	GetOrCreateChannel(ctx context.Context, ownerID int64, now time.Time) (Channel, error)
	// FindChannel mengambil pengaturan siaran. ErrNotFound jika belum ada.
	FindChannel(ctx context.Context, ownerID int64) (Channel, error)
	// UpdateChannel mengubah judul dan kategori, sekaligus siaran yang
	// sedang LIVE milik user itu (satu transaksi).
	UpdateChannel(ctx context.Context, ownerID int64, title, category string, now time.Time) (Channel, error)
	// SetStreamKey menyimpan hash stream key baru.
	SetStreamKey(ctx context.Context, ownerID int64, hash string, now time.Time) (Channel, error)
	// CurrentStream mengambil siaran LIVE milik user. ErrNotFound jika
	// tidak sedang live.
	CurrentStream(ctx context.Context, ownerID int64) (Stream, error)
	// FindStream mengambil satu siaran. ErrNotFound jika tidak ada.
	FindStream(ctx context.Context, id int64) (Stream, error)
	// ListLive mengambil siaran LIVE, terbaru dulu.
	ListLive(ctx context.Context, f ListFilter) ([]Stream, error)
	// Sync menyamakan tabel live_streams dengan daftar stream dari MediaMTX:
	//   - kreator di online yang belum punya siaran LIVE: siaran baru dibuat;
	//   - yang sudah punya: last_seen_at dan jumlah penonton diperbarui;
	//   - siaran LIVE yang last_seen_at-nya lebih lama dari staleBefore:
	//     ditutup (ENDED, ended_at = last_seen_at).
	Sync(ctx context.Context, online []Online, now, staleBefore time.Time) (started, ended int, err error)
	// SetPublishToken menyimpan hash token siaran browser beserta batas
	// waktunya, menggantikan token sebelumnya.
	SetPublishToken(ctx context.Context, ownerID int64, hash string, expiresAt, now time.Time) error
	// CreateChat menyimpan pesan chat dan mengisi ID-nya.
	CreateChat(ctx context.Context, m ChatMessage) (ChatMessage, error)
	// ListChat mengambil limit pesan terakhir sebuah siaran, urut dari yang
	// paling lama.
	ListChat(ctx context.Context, streamID int64, limit int) ([]ChatMessage, error)
	// CountChatSince menghitung pesan chat userID sesudah waktu since.
	CountChatSince(ctx context.Context, userID int64, since time.Time) (int64, error)
}

// ChatPublisher menyiarkan pesan chat yang baru tersimpan ke penonton lain.
// Implementasi NATS ada di nats.go.
type ChatPublisher interface {
	PublishChat(ctx context.Context, m ChatMessage) error
}

// Options adalah pengaturan Service.
type Options struct {
	// IngestURL adalah alamat RTMP untuk OBS, misalnya
	// "rtmp://localhost:1935/live".
	IngestURL string
	// PlaybackBaseURL adalah alamat publik HLS MediaMTX, misalnya
	// "http://live.ice-stream.localhost:4000". URL putar =
	// PlaybackBaseURL + "/live/<owner_id>/index.m3u8".
	PlaybackBaseURL string
	// Grace adalah batas tenggang: stream yang hilang lebih singkat dari
	// ini dianggap siaran yang sama (koneksi kreator sempat putus).
	Grace time.Duration
	// WHIPBaseURL adalah alamat publik WebRTC MediaMTX untuk siaran dari
	// browser, misalnya "http://webrtc.ice-stream.localhost:4000". Alamat
	// WHIP = WHIPBaseURL + "/live/<owner_id>/whip".
	WHIPBaseURL string
	// PublishTokenTTL adalah lama token siaran browser berlaku. Token hanya
	// diperiksa saat siaran dimulai, jadi cukup beberapa menit.
	PublishTokenTTL time.Duration
	// Publisher menyiarkan chat baru. Boleh nil (test): chat tetap
	// tersimpan, hanya tidak disiarkan.
	Publisher ChatPublisher
	// Logger boleh nil.
	Logger *slog.Logger
}

// Service berisi aturan bisnis LiveService.
type Service struct {
	repo Repository
	opts Options
	now  func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, opts Options) *Service {
	opts.PlaybackBaseURL = strings.TrimRight(opts.PlaybackBaseURL, "/")
	opts.WHIPBaseURL = strings.TrimRight(opts.WHIPBaseURL, "/")
	if opts.Grace <= 0 {
		opts.Grace = 15 * time.Second
	}
	if opts.PublishTokenTTL <= 0 {
		opts.PublishTokenTTL = 5 * time.Minute
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{repo: repo, opts: opts, now: func() time.Time { return time.Now().UTC() }}
}

// ChannelView adalah pengaturan siaran beserta siaran yang sedang berjalan.
type ChannelView struct {
	Channel
	IngestURL string
	// Current nil jika tidak sedang live.
	Current *Stream
}

const (
	maxTitleRunes = 100
	maxQueryRunes = 100
	// Pesan chat mengikuti aturan danmaku: satu baris, maksimal 100 huruf.
	maxChatRunes = 100
	// Mode lambat chat: satu pesan per 3 detik per user (desain Live.dc.html).
	chatSlowMode = 3 * time.Second
	// Awalan rahasia: stream key OBS dan token siaran browser.
	keyPrefix   = "isk_"
	tokenPrefix = "ipt_"
)

// GetMyChannel mengambil (atau membuat) pengaturan siaran requester.
func (s *Service) GetMyChannel(ctx context.Context, requesterID int64) (ChannelView, error) {
	ch, err := s.repo.GetOrCreateChannel(ctx, requesterID, s.now())
	if err != nil {
		return ChannelView{}, err
	}
	return s.view(ctx, ch)
}

// UpdateMyChannel mengubah judul dan kategori siaran.
func (s *Service) UpdateMyChannel(ctx context.Context, requesterID int64, title, category string) (ChannelView, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return ChannelView{}, &ValidationError{"title", "judul siaran wajib diisi"}
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return ChannelView{}, &ValidationError{"title", "maksimal 100 karakter"}
	}
	category = strings.ToLower(strings.TrimSpace(category))
	if category != "" && !isCategory(category) {
		return ChannelView{}, &ValidationError{"category", "harus salah satu dari: " + strings.Join(video.Categories, ", ")}
	}
	now := s.now()
	// Pastikan barisnya ada dulu, supaya UpdateChannel cukup UPDATE.
	if _, err := s.repo.GetOrCreateChannel(ctx, requesterID, now); err != nil {
		return ChannelView{}, err
	}
	ch, err := s.repo.UpdateChannel(ctx, requesterID, title, category, now)
	if err != nil {
		return ChannelView{}, err
	}
	return s.view(ctx, ch)
}

// ResetStreamKey membuat stream key baru dan mengembalikan teks yang
// ditempel di kolom "Stream Key" OBS: "<owner_id>?key=<rahasia>".
//
// Kenapa bentuknya begitu? OBS menggabungkan Server + "/" + Stream Key
// menjadi rtmp://localhost:1935/live/7?key=isk_.... MediaMTX membaca
// bagian path ("live/7") sebagai nama stream yang ditonton penonton, dan
// query ("key=...") hanya dikirim ke hook auth. Jadi rahasianya tidak
// pernah muncul di alamat HLS yang dibuka penonton.
func (s *Service) ResetStreamKey(ctx context.Context, requesterID int64) (ChannelView, string, error) {
	now := s.now()
	if _, err := s.repo.GetOrCreateChannel(ctx, requesterID, now); err != nil {
		return ChannelView{}, "", err
	}
	secret, err := newSecret(keyPrefix)
	if err != nil {
		return ChannelView{}, "", err
	}
	ch, err := s.repo.SetStreamKey(ctx, requesterID, hashSecret(secret), now)
	if err != nil {
		return ChannelView{}, "", err
	}
	v, err := s.view(ctx, ch)
	if err != nil {
		return ChannelView{}, "", err
	}
	return v, strconv.FormatInt(requesterID, 10) + "?key=" + secret, nil
}

// ListLive mengambil siaran yang sedang LIVE.
func (s *Service) ListLive(ctx context.Context, f ListFilter) ([]Stream, error) {
	switch {
	case f.Limit == 0:
		f.Limit = 20
	case f.Limit < 1 || f.Limit > 50:
		return nil, &ValidationError{"limit", "harus antara 1 dan 50"}
	}
	f.Category = strings.ToLower(strings.TrimSpace(f.Category))
	if f.Category != "" && !isCategory(f.Category) {
		return nil, &ValidationError{"category", "harus salah satu dari: " + strings.Join(video.Categories, ", ")}
	}
	if len(f.OwnerIDs) > 100 {
		return nil, &ValidationError{"owner_ids", "maksimal 100"}
	}
	f.Query = strings.Join(strings.Fields(f.Query), " ")
	if utf8.RuneCountInString(f.Query) > maxQueryRunes {
		return nil, &ValidationError{"query", "maksimal 100 karakter"}
	}
	return s.repo.ListLive(ctx, f)
}

// GetStream mengambil satu siaran.
func (s *Service) GetStream(ctx context.Context, id int64) (Stream, error) {
	if id <= 0 {
		return Stream{}, ErrNotFound
	}
	return s.repo.FindStream(ctx, id)
}

// PlaybackURL adalah alamat HLS siaran. Kosong jika sudah selesai.
func (s *Service) PlaybackURL(st Stream) string {
	if st.Status != StatusLive {
		return ""
	}
	return s.opts.PlaybackBaseURL + "/" + PathFor(st.OwnerID) + "/index.m3u8"
}

// AuthorizePublish dipanggil hook MediaMTX saat kreator mulai mengirim
// video. Ada dua cara yang diterima:
//   - OBS (RTMP): stream key di query, "key=isk_...";
//   - browser (WHIP): token pendek dari CreatePublishToken di header
//     Authorization, yang diteruskan MediaMTX sebagai Token.
func (s *Service) AuthorizePublish(ctx context.Context, req PublishRequest) (int64, error) {
	ownerID, ok := OwnerFromPath(req.Path)
	if !ok {
		return 0, ErrForbidden
	}
	// Token siaran browser hanya diterima lewat header Authorization (WHIP),
	// tidak lewat query RTMP. Stream key boleh lewat keduanya.
	secret, fromHeader := req.Token, req.Token != ""
	if !fromHeader {
		q, err := url.ParseQuery(req.Query)
		if err != nil {
			return 0, ErrForbidden
		}
		secret = q.Get("key")
	}
	if secret == "" {
		return 0, ErrForbidden
	}
	ch, err := s.repo.FindChannel(ctx, ownerID)
	if errors.Is(err, ErrNotFound) {
		return 0, ErrForbidden
	}
	if err != nil {
		return 0, err
	}
	switch {
	case strings.HasPrefix(secret, keyPrefix) && sameHash(ch.StreamKeyHash, secret):
		return ownerID, nil
	case fromHeader && strings.HasPrefix(secret, tokenPrefix) && sameHash(ch.PublishTokenHash, secret) && s.now().Before(ch.PublishTokenExpiresAt):
		return ownerID, nil
	default:
		return 0, ErrForbidden
	}
}

// PublishToken adalah jawaban CreatePublishToken.
type PublishToken struct {
	WHIPURL   string
	Token     string
	ExpiresAt time.Time
}

// CreatePublishToken membuat token siaran browser untuk requester.
func (s *Service) CreatePublishToken(ctx context.Context, requesterID int64) (PublishToken, error) {
	now := s.now()
	if _, err := s.repo.GetOrCreateChannel(ctx, requesterID, now); err != nil {
		return PublishToken{}, err
	}
	token, err := newSecret(tokenPrefix)
	if err != nil {
		return PublishToken{}, err
	}
	expires := now.Add(s.opts.PublishTokenTTL)
	if err := s.repo.SetPublishToken(ctx, requesterID, hashSecret(token), expires, now); err != nil {
		return PublishToken{}, err
	}
	return PublishToken{
		WHIPURL:   s.opts.WHIPBaseURL + "/" + PathFor(requesterID) + "/whip",
		Token:     token,
		ExpiresAt: expires,
	}, nil
}

// SendChatInput adalah isi SendChatMessage.
type SendChatInput struct {
	RequesterID int64
	StreamID    int64
	Body        string
	// Color nil = putih.
	Color *uint32
}

// SendChat memeriksa dan menyimpan pesan chat, lalu menyiarkannya. Siaran
// bersifat "best effort", sama seperti danmaku: jika NATS sedang mati,
// pesan tetap tersimpan dan terlihat oleh penonton yang memuat ulang.
func (s *Service) SendChat(ctx context.Context, in SendChatInput) (ChatMessage, error) {
	if in.RequesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return ChatMessage{}, errors.New("requester id kosong")
	}
	body, err := textrule.SingleLine(in.Body, maxChatRunes)
	if err != nil {
		return ChatMessage{}, &ValidationError{"body", err.Error()}
	}
	color := ColorWhite
	if in.Color != nil {
		if *in.Color > ColorWhite {
			return ChatMessage{}, &ValidationError{"color", "harus warna RGB 0x000000 sampai 0xFFFFFF"}
		}
		color = *in.Color
	}
	st, err := s.GetStream(ctx, in.StreamID)
	if err != nil {
		return ChatMessage{}, err
	}
	if st.Status != StatusLive {
		return ChatMessage{}, ErrStreamEnded
	}
	now := s.now()
	// Mode lambat: satu pesan per chatSlowMode per user. Dua request yang
	// datang bersamaan bisa sama-sama lolos; untuk mencegah spam itu cukup.
	recent, err := s.repo.CountChatSince(ctx, in.RequesterID, now.Add(-chatSlowMode))
	if err != nil {
		return ChatMessage{}, err
	}
	if recent > 0 {
		return ChatMessage{}, ErrRateLimited
	}
	saved, err := s.repo.CreateChat(ctx, ChatMessage{StreamID: st.ID, UserID: in.RequesterID, Body: body, Color: color, CreatedAt: now})
	if err != nil {
		return ChatMessage{}, err
	}
	if s.opts.Publisher != nil {
		if err := s.opts.Publisher.PublishChat(ctx, saved); err != nil {
			s.opts.Logger.WarnContext(ctx, "chat tersimpan tapi gagal disiarkan",
				slog.Int64("message_id", saved.ID), slog.Any("error", err))
		}
	}
	return saved, nil
}

// ListChat mengambil pesan terakhir sebuah siaran (LIVE atau ENDED).
func (s *Service) ListChat(ctx context.Context, streamID int64, limit int) ([]ChatMessage, error) {
	switch {
	case limit == 0:
		limit = 50
	case limit < 1 || limit > 100:
		return nil, &ValidationError{"limit", "harus antara 1 dan 100"}
	}
	if _, err := s.GetStream(ctx, streamID); err != nil {
		return nil, err
	}
	return s.repo.ListChat(ctx, streamID, limit)
}

// Sync menyamakan status siaran dengan daftar stream dari MediaMTX.
func (s *Service) Sync(ctx context.Context, online []Online) (started, ended int, err error) {
	now := s.now()
	return s.repo.Sync(ctx, online, now, now.Add(-s.opts.Grace))
}

func (s *Service) view(ctx context.Context, ch Channel) (ChannelView, error) {
	v := ChannelView{Channel: ch, IngestURL: s.opts.IngestURL}
	cur, err := s.repo.CurrentStream(ctx, ch.OwnerID)
	switch {
	case err == nil:
		v.Current = &cur
	case !errors.Is(err, ErrNotFound):
		return ChannelView{}, err
	}
	return v, nil
}

// pathPrefix adalah awalan nama stream di MediaMTX. Harus sama dengan
// pola "paths" di infra/k8s/live/configmap.yaml.
const pathPrefix = "live/"

// PathFor mengembalikan nama stream MediaMTX milik user, misalnya "live/7".
func PathFor(ownerID int64) string {
	return pathPrefix + strconv.FormatInt(ownerID, 10)
}

// OwnerFromPath kebalikan PathFor. ok=false untuk nama lain.
func OwnerFromPath(path string) (int64, bool) {
	rest, found := strings.CutPrefix(path, pathPrefix)
	if !found || rest == "" || strings.HasPrefix(rest, "0") {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// newSecret membuat rahasia: awalan + 32 byte acak (base64url). Awalan
// "isk_" (Ice-Stream Key) dan "ipt_" (Ice-Stream Publish Token) memudahkan
// alat pemindai rahasia, seperti GitHub secret scanning, mengenali rahasia
// yang tidak sengaja di-commit.
func newSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("membuat rahasia: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// sameHash membandingkan hash tersimpan dengan hash secret.
// ConstantTimeCompare: lama perbandingan tidak bergantung pada berapa huruf
// yang cocok, jadi rahasia tidak bisa ditebak dari waktu respons.
func sameHash(stored, secret string) bool {
	return stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(hashSecret(secret))) == 1
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func isCategory(c string) bool {
	for _, known := range video.Categories {
		if c == known {
			return true
		}
	}
	return false
}
