package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Repository adalah akses database yang dibutuhkan Service. Implementasi
// Oracle ada di oracle_repository.go; test memakai versi palsu.
type Repository interface {
	// FindByID dan FindByUsername mengembalikan ErrNotFound jika tidak ada.
	FindByID(ctx context.Context, id int64) (Profile, error)
	FindByUsername(ctx context.Context, username string) (Profile, error)
	// FindByIDs melewati id yang tidak ada.
	FindByIDs(ctx context.Context, ids []int64) ([]Profile, error)
	UpdateProfile(ctx context.Context, id int64, u ProfileUpdate, now time.Time) (Profile, error)
	// SetAvatarKey mengganti avatar_key (kosong = hapus) dan mengembalikan
	// key sebelumnya.
	SetAvatarKey(ctx context.Context, id int64, key string, now time.Time) (string, error)
	// Subscribe dan Unsubscribe aman dipanggil ulang dan mengembalikan
	// subscriber_count terbaru. Subscribe: ErrNotFound jika channel tidak
	// ada atau diblokir, ErrTooManySubscriptions jika sudah maxSubs.
	Subscribe(ctx context.Context, subscriberID, channelID int64, maxSubs int) (int64, error)
	Unsubscribe(ctx context.Context, subscriberID, channelID int64) (int64, error)
	IsSubscribed(ctx context.Context, subscriberID, channelID int64) (bool, error)
	// ListSubscriptions: terbaru lebih dulu, id lebih kecil dari beforeID
	// (0 = dari yang terbaru).
	ListSubscriptions(ctx context.Context, subscriberID, beforeID int64, limit int) ([]Subscription, error)
	ListSubscribedChannelIDs(ctx context.Context, subscriberID int64, limit int) ([]int64, error)
}

// ObjectStore adalah penyimpanan file foto profil. Implementasi S3 ada di
// storage.go; test memakai versi palsu.
type ObjectStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
}

const (
	maxDisplayNameRunes = 50
	maxBioRunes         = 500
	// maxBatchIDs membatasi BatchGetProfiles. Satu halaman feed paling
	// banyak 50 video, jadi 100 sudah longgar.
	maxBatchIDs = 100
	// maxSubscriptions membatasi jumlah channel yang boleh diikuti satu
	// user (Bilibili memakai batas serupa). Batas ini juga menjaga query
	// feed subscription tetap di bawah batas IN (...) Oracle, 1000 nilai.
	maxSubscriptions        = 1000
	defaultSubscriptionPage = 20
	maxSubscriptionPage     = 50
)

// Service berisi aturan bisnis UserService.
type Service struct {
	repo   Repository
	store  ObjectStore
	logger *slog.Logger
	// mediaBaseURL misalnya "http://media.ice-stream.localhost:4000/images".
	mediaBaseURL string
	now          func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, store ObjectStore, logger *slog.Logger, mediaBaseURL string) *Service {
	return &Service{
		repo:         repo,
		store:        store,
		logger:       logger,
		mediaBaseURL: mediaBaseURL,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// GetProfile mengambil profil berdasarkan id (jika userID > 0) atau
// username. Email hanya disertakan jika viewerID adalah user itu sendiri.
// Akun yang diblokir hanya terlihat oleh pemiliknya.
func (s *Service) GetProfile(ctx context.Context, viewerID, userID int64, username string) (Profile, error) {
	var p Profile
	var err error
	switch {
	case userID > 0:
		p, err = s.repo.FindByID(ctx, userID)
	case username != "":
		// Username selalu disimpan huruf kecil (lihat auth.Register).
		p, err = s.repo.FindByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	default:
		return Profile{}, &ValidationError{"user", "isi user_id atau username"}
	}
	if err != nil {
		return Profile{}, err
	}
	isSelf := viewerID != 0 && viewerID == p.ID
	if p.Status == statusBanned && !isSelf {
		return Profile{}, ErrNotFound
	}
	if !isSelf {
		p.Email = ""
	}
	return p, nil
}

// BatchGetProfiles mengambil profil publik banyak user sekaligus. Id dobel
// dan id <= 0 dibuang; akun yang diblokir dilewati. Email tidak pernah
// disertakan.
func (s *Service) BatchGetProfiles(ctx context.Context, ids []int64) ([]Profile, error) {
	seen := make(map[int64]bool, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) > maxBatchIDs {
		return nil, &ValidationError{"user_ids", fmt.Sprintf("maksimal %d id", maxBatchIDs)}
	}
	profiles, err := s.repo.FindByIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	out := profiles[:0]
	for _, p := range profiles {
		if p.Status != statusBanned {
			p.Email = ""
			out = append(out, p)
		}
	}
	return out, nil
}

// UpdateProfile mengubah nama tampilan dan/atau bio milik requesterID.
func (s *Service) UpdateProfile(ctx context.Context, requesterID int64, u ProfileUpdate) (Profile, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Profile{}, errors.New("requester id kosong")
	}
	if u.DisplayName != nil {
		v := strings.TrimSpace(*u.DisplayName)
		if v == "" || utf8.RuneCountInString(v) > maxDisplayNameRunes {
			return Profile{}, &ValidationError{"display_name", fmt.Sprintf("wajib diisi, maksimal %d karakter", maxDisplayNameRunes)}
		}
		u.DisplayName = &v
	}
	if u.Bio != nil {
		v := strings.TrimSpace(*u.Bio)
		if utf8.RuneCountInString(v) > maxBioRunes {
			return Profile{}, &ValidationError{"bio", fmt.Sprintf("maksimal %d karakter", maxBioRunes)}
		}
		u.Bio = &v
	}
	if u.DisplayName == nil && u.Bio == nil {
		// Tidak ada yang diubah: kembalikan profil apa adanya.
		return s.GetProfile(ctx, requesterID, requesterID, "")
	}
	return s.repo.UpdateProfile(ctx, requesterID, u, s.now())
}

// UploadAvatar memproses gambar (lihat ProcessAvatar), menyimpannya dengan
// nama acak baru, lalu menghapus foto lama.
//
// Urutannya: simpan file baru, ubah database, baru hapus file lama. Kalau
// langkah di tengah gagal, paling buruk ada file yatim di storage, tapi
// profil tidak pernah menunjuk ke file yang sudah dihapus.
func (s *Service) UploadAvatar(ctx context.Context, requesterID int64, data []byte) (Profile, error) {
	if requesterID <= 0 {
		return Profile{}, errors.New("requester id kosong")
	}
	img, err := ProcessAvatar(data)
	if err != nil {
		return Profile{}, err
	}
	// Pastikan user ada sebelum menyimpan file ke storage.
	if _, err := s.repo.FindByID(ctx, requesterID); err != nil {
		return Profile{}, err
	}
	key, err := newAvatarKey(requesterID)
	if err != nil {
		return Profile{}, err
	}
	if err := s.store.Put(ctx, key, "image/jpeg", img); err != nil {
		return Profile{}, err
	}
	old, err := s.repo.SetAvatarKey(ctx, requesterID, key, s.now())
	if err != nil {
		s.deleteQuietly(ctx, key)
		return Profile{}, err
	}
	s.deleteQuietly(ctx, old)
	return s.GetProfile(ctx, requesterID, requesterID, "")
}

// RemoveAvatar menghapus foto profil requesterID (tidak error jika memang
// belum punya).
func (s *Service) RemoveAvatar(ctx context.Context, requesterID int64) (Profile, error) {
	if requesterID <= 0 {
		return Profile{}, errors.New("requester id kosong")
	}
	old, err := s.repo.SetAvatarKey(ctx, requesterID, "", s.now())
	if err != nil {
		return Profile{}, err
	}
	s.deleteQuietly(ctx, old)
	return s.GetProfile(ctx, requesterID, requesterID, "")
}

// deleteQuietly menghapus file tanpa menggagalkan request. File yang gagal
// dihapus hanya memakan sedikit ruang (sekitar 20 KB), jadi cukup dicatat
// di log.
func (s *Service) deleteQuietly(ctx context.Context, key string) {
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.Delete(ctx, key); err != nil {
		s.logger.WarnContext(ctx, "gagal menghapus foto profil lama", slog.String("key", key), slog.Any("error", err))
	}
}

// AvatarURL mengubah key menjadi URL publik. Kosong jika key kosong atau di
// luar AvatarPrefix.
func (s *Service) AvatarURL(key string) string {
	if !strings.HasPrefix(key, AvatarPrefix) {
		return ""
	}
	return s.mediaBaseURL + "/" + key
}

// newAvatarKey membuat nama file acak, misalnya "avatars/7/3f9c0a1b2c3d4e5f.jpg".
// Nama selalu baru, jadi cache browser lama tidak menampilkan foto lama.
func newAvatarKey(userID int64) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("membuat nama file avatar: %w", err)
	}
	return AvatarPrefix + strconv.FormatInt(userID, 10) + "/" + hex.EncodeToString(b) + ".jpg", nil
}

// ViewerSubscribed mengembalikan true jika viewerID mengikuti channelID.
// Selalu false untuk pengunjung tanpa login dan untuk channel sendiri.
func (s *Service) ViewerSubscribed(ctx context.Context, viewerID, channelID int64) (bool, error) {
	if viewerID <= 0 || viewerID == channelID {
		return false, nil
	}
	return s.repo.IsSubscribed(ctx, viewerID, channelID)
}

// SubscribeResult adalah hasil Subscribe dan Unsubscribe.
type SubscribeResult struct {
	Subscribed      bool
	SubscriberCount int64
}

// Subscribe membuat requesterID mengikuti channelID. Aman dipanggil ulang.
func (s *Service) Subscribe(ctx context.Context, requesterID, channelID int64) (SubscribeResult, error) {
	if requesterID <= 0 {
		return SubscribeResult{}, errors.New("requester id kosong")
	}
	if channelID <= 0 {
		return SubscribeResult{}, ErrNotFound
	}
	if channelID == requesterID {
		return SubscribeResult{}, &ValidationError{"channel_id", "tidak bisa subscribe channel sendiri"}
	}
	count, err := s.repo.Subscribe(ctx, requesterID, channelID, maxSubscriptions)
	if err != nil {
		return SubscribeResult{}, err
	}
	return SubscribeResult{Subscribed: true, SubscriberCount: count}, nil
}

// Unsubscribe membuat requesterID berhenti mengikuti channelID.
func (s *Service) Unsubscribe(ctx context.Context, requesterID, channelID int64) (SubscribeResult, error) {
	if requesterID <= 0 {
		return SubscribeResult{}, errors.New("requester id kosong")
	}
	if channelID <= 0 {
		return SubscribeResult{}, ErrNotFound
	}
	count, err := s.repo.Unsubscribe(ctx, requesterID, channelID)
	if err != nil {
		return SubscribeResult{}, err
	}
	return SubscribeResult{Subscribed: false, SubscriberCount: count}, nil
}

// ListSubscriptions mengambil profil channel yang diikuti requesterID, yang
// terakhir di-subscribe lebih dulu. Channel yang diblokir dilewati, jadi
// satu halaman bisa berisi kurang dari pageSize walau masih ada halaman
// berikutnya.
func (s *Service) ListSubscriptions(ctx context.Context, requesterID int64, pageSize int, pageToken string) ([]Profile, string, error) {
	if requesterID <= 0 {
		return nil, "", errors.New("requester id kosong")
	}
	if pageSize <= 0 {
		pageSize = defaultSubscriptionPage
	}
	if pageSize > maxSubscriptionPage {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxSubscriptionPage)}
	}
	var beforeID int64
	if pageToken != "" {
		id, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		beforeID = id
	}
	subs, err := s.repo.ListSubscriptions(ctx, requesterID, beforeID, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(subs) > pageSize {
		subs = subs[:pageSize]
		next = encodeCursor(subs[pageSize-1].ID)
	}
	ids := make([]int64, len(subs))
	for i, sub := range subs {
		ids[i] = sub.ChannelID
	}
	profiles, err := s.BatchGetProfiles(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	// FindByIDs tidak menjamin urutan; susun ulang sesuai urutan subscription.
	byID := make(map[int64]Profile, len(profiles))
	for _, p := range profiles {
		byID[p.ID] = p
	}
	out := make([]Profile, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out, next, nil
}

// ListSubscribedChannelIDs mengambil semua id channel yang diikuti
// requesterID (maksimal maxSubscriptions), untuk feed subscription.
func (s *Service) ListSubscribedChannelIDs(ctx context.Context, requesterID int64) ([]int64, error) {
	if requesterID <= 0 {
		return nil, errors.New("requester id kosong")
	}
	return s.repo.ListSubscribedChannelIDs(ctx, requesterID, maxSubscriptions)
}

// encodeCursor dan decodeCursor: token halaman berikutnya berupa base64
// dari id subscription terakhir, sama seperti komentar di VideoService.
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCursor(token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id cursor salah")
	}
	return id, nil
}
