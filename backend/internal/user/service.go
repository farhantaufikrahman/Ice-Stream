package user

import (
	"context"
	"crypto/rand"
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
