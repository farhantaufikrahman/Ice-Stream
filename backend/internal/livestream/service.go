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
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	if opts.Grace <= 0 {
		opts.Grace = 15 * time.Second
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

const maxTitleRunes = 100

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
	secret, err := newSecret()
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

// AuthorizePublish dipanggil hook MediaMTX saat OBS mulai mengirim video.
// path adalah nama stream ("live/7"), query adalah sisa URL ("key=...").
func (s *Service) AuthorizePublish(ctx context.Context, path, query string) (int64, error) {
	ownerID, ok := OwnerFromPath(path)
	if !ok {
		return 0, ErrForbidden
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return 0, ErrForbidden
	}
	secret := q.Get("key")
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
	// ConstantTimeCompare: lama perbandingan tidak bergantung pada berapa
	// huruf yang cocok, jadi key tidak bisa ditebak dari waktu respons.
	if ch.StreamKeyHash == "" || subtle.ConstantTimeCompare([]byte(ch.StreamKeyHash), []byte(hashSecret(secret))) != 1 {
		return 0, ErrForbidden
	}
	return ownerID, nil
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

// newSecret membuat rahasia stream key: "isk_" + 32 byte acak (base64url).
// Awalan "isk_" (Ice-Stream Key) memudahkan alat pemindai rahasia, seperti
// GitHub secret scanning, mengenali key yang tidak sengaja di-commit.
func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("membuat stream key: %w", err)
	}
	return "isk_" + base64.RawURLEncoding.EncodeToString(b), nil
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
