package video

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	// Import dengan "_" hanya untuk mendaftarkan decoder ke image.Decode:
	// PNG di sini, WebP di bawah. JPEG sudah terdaftar lewat image/jpeg.
	_ "image/png"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Thumbnail pilihan kreator (langkah 26, desain Upload: "Bingkai 00:45",
// "Bingkai 02:14", "Bingkai 07:30", "Unggah gambar").
//
// Transcoder membuat beberapa bingkai di folder "frames/" di samping
// master.m3u8 (lihat FrameKey). Kreator memilih salah satunya, atau
// mengunggah gambar sendiri. Pilihannya disimpan di kolom thumbnail_key.

const (
	// MaxThumbnailBytes adalah ukuran file thumbnail terbesar yang
	// diterima, sama dengan batas foto profil.
	MaxThumbnailBytes = 2 << 20 // 2 MiB
	// Ukuran hasil proses: 16:9, sama dengan pemutar video. 1280x720 adalah
	// ukuran thumbnail yang disarankan YouTube.
	thumbnailWidth  = 1280
	thumbnailHeight = 720
	// Batas ukuran gambar sebelum diproses, alasannya sama dengan foto
	// profil (lihat internal/user/avatar.go): gambar PNG kecil bisa berisi
	// jutaan piksel yang menghabiskan RAM saat dibuka.
	minThumbnailWidth  = 320
	minThumbnailHeight = 180
	maxThumbnailPixels = 25_000_000
	thumbnailQuality   = 85
)

var allowedThumbnailTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// ErrFrameNotFound: posisi bingkai yang dipilih tidak ada di daftar bingkai
// video ini.
var ErrFrameNotFound = errors.New("bingkai thumbnail tidak ditemukan")

// FrameKey mengembalikan lokasi file bingkai thumbnail di posisi ms,
// misalnya "hls/42/9f2c.../frames/64000.jpg". Kosong jika video belum
// punya playlist.
func FrameKey(v Video, ms int64) string {
	if v.PlaylistKey == "" {
		return ""
	}
	return path.Dir(v.PlaylistKey) + "/frames/" + strconv.FormatInt(ms, 10) + ".jpg"
}

// isCustomThumbnail: key milik thumbnail yang diunggah kreator (lihat
// customThumbnailKey), bukan buatan transcoder.
func isCustomThumbnail(v Video, key string) bool {
	return strings.HasPrefix(key, PublicPrefix+strconv.FormatInt(v.ID, 10)+"/custom-")
}

// customThumbnailKey membuat nama file acak, misalnya
// "hls/42/custom-3f9c....jpg". Acak supaya tidak bisa ditebak, dan supaya
// setiap gambar baru punya alamat baru (cache browser tidak perlu dihapus).
func customThumbnailKey(videoID int64) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return PublicPrefix + strconv.FormatInt(videoID, 10) + "/custom-" + hex.EncodeToString(b) + ".jpg", nil
}

// ProcessThumbnail memeriksa file gambar, memotongnya menjadi 16:9 di
// tengah, mengubah ukurannya menjadi 1280x720, lalu menyimpannya ulang
// sebagai JPEG. Alasannya sama dengan foto profil: metadata EXIF (termasuk
// lokasi GPS) terbuang, file palsu tidak lolos, dan ukurannya seragam.
func ProcessThumbnail(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, &ValidationError{"image", "file kosong"}
	}
	if len(data) > MaxThumbnailBytes {
		return nil, &ValidationError{"image", "maksimal 2 MiB"}
	}
	if ct := http.DetectContentType(data); !allowedThumbnailTypes[ct] {
		return nil, &ValidationError{"image", "harus berupa gambar JPEG, PNG, atau WebP"}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, &ValidationError{"image", "gambar tidak bisa dibaca"}
	}
	if cfg.Width < minThumbnailWidth || cfg.Height < minThumbnailHeight {
		return nil, &ValidationError{"image", fmt.Sprintf("minimal %dx%d piksel", minThumbnailWidth, minThumbnailHeight)}
	}
	if cfg.Width*cfg.Height > maxThumbnailPixels {
		return nil, &ValidationError{"image", "ukuran gambar terlalu besar (maksimal 25 megapiksel)"}
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &ValidationError{"image", "gambar tidak bisa dibaca"}
	}
	dst := image.NewRGBA(image.Rect(0, 0, thumbnailWidth, thumbnailHeight))
	// Latar hitam untuk bagian transparan PNG/WebP, cocok dengan pemutar.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, center16x9(src.Bounds()), draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return out.Bytes(), nil
}

// center16x9 mengambil area 16:9 terbesar di tengah gambar. Gambar
// 1000x1000 menjadi area 1000x562 mulai dari y=219.
func center16x9(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	if w*9 > h*16 {
		// Terlalu lebar: potong kiri dan kanan.
		nw := h * 16 / 9
		x := b.Min.X + (w-nw)/2
		return image.Rect(x, b.Min.Y, x+nw, b.Max.Y)
	}
	nh := w * 9 / 16
	y := b.Min.Y + (h-nh)/2
	return image.Rect(b.Min.X, y, b.Max.X, y+nh)
}

// ownedVideo mengambil video milik requesterID. Video orang lain dijawab
// ErrNotFound, sama seperti video yang tidak ada.
func (s *Service) ownedVideo(ctx context.Context, requesterID, videoID int64) (Video, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Video{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return Video{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return Video{}, err
	}
	if v.OwnerID != requesterID {
		return Video{}, ErrNotFound
	}
	return v, nil
}

// ChooseThumbnailFrame memakai bingkai di posisi positionMs sebagai
// thumbnail. Posisinya harus salah satu dari v.ThumbnailFrames.
func (s *Service) ChooseThumbnailFrame(ctx context.Context, requesterID, videoID, positionMs int64) (Video, error) {
	v, err := s.ownedVideo(ctx, requesterID, videoID)
	if err != nil {
		return Video{}, err
	}
	if v.Status != StatusReady || !slices.Contains(v.ThumbnailFrames, positionMs) {
		return Video{}, ErrFrameNotFound
	}
	return s.replaceThumbnail(ctx, v, FrameKey(v, positionMs))
}

// UploadThumbnail menyimpan gambar buatan kreator sebagai thumbnail. Boleh
// sebelum video selesai diproses: MarkReady tidak menimpa thumbnail yang
// sudah ada.
func (s *Service) UploadThumbnail(ctx context.Context, requesterID, videoID int64, data []byte) (Video, error) {
	v, err := s.ownedVideo(ctx, requesterID, videoID)
	if err != nil {
		return Video{}, err
	}
	img, err := ProcessThumbnail(data)
	if err != nil {
		return Video{}, err
	}
	key, err := customThumbnailKey(v.ID)
	if err != nil {
		return Video{}, err
	}
	if err := s.store.Put(ctx, key, "image/jpeg", img); err != nil {
		return Video{}, err
	}
	updated, err := s.replaceThumbnail(ctx, v, key)
	if err != nil {
		// File baru tidak terpakai; hapus supaya tidak menumpuk.
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		return Video{}, err
	}
	return updated, nil
}

// replaceThumbnail menyimpan key baru, lalu menghapus gambar buatan kreator
// yang lama (jika ada). Bingkai buatan transcoder tidak dihapus, supaya
// tetap bisa dipilih lagi.
func (s *Service) replaceThumbnail(ctx context.Context, v Video, key string) (Video, error) {
	updated, err := s.repo.SetThumbnailKey(ctx, v.ID, v.OwnerID, key, s.now())
	if err != nil {
		return Video{}, err
	}
	if old := v.ThumbnailKey; old != key && isCustomThumbnail(v, old) {
		// Gagal menghapus file lama tidak membatalkan penggantian: file itu
		// tetap terhapus saat videonya dihapus (PurgeMedia, hls/<id>/).
		_ = s.store.Delete(ctx, old)
	}
	return updated, nil
}
