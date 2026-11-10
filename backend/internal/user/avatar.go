package user

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	// Import dengan "_" hanya untuk mendaftarkan decoder ke image.Decode:
	// PNG di sini, WebP di bawah. JPEG sudah terdaftar lewat image/jpeg.
	_ "image/png"
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// MaxAvatarBytes adalah ukuran file foto profil terbesar yang diterima.
	MaxAvatarBytes = 2 << 20 // 2 MiB
	// AvatarSize adalah sisi foto profil hasil proses (persegi).
	AvatarSize = 256
	// Batas ukuran gambar sebelum diproses. File PNG 2 MiB bisa berisi
	// gambar 20000x20000 piksel (sebagian besar warna polos) yang butuh RAM
	// 1,6 GB saat dibuka. Ukurannya dicek dulu dari header, sebelum isi
	// gambar dibaca, supaya service tidak kehabisan memori.
	minAvatarSide   = 64
	maxAvatarPixels = 25_000_000 // 25 megapiksel, misalnya 5000x5000
	avatarQuality   = 85
)

// allowedAvatarTypes adalah format yang diterima, dikenali dari isi file
// (bukan dari nama file atau header Content-Type yang bisa dipalsukan).
var allowedAvatarTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// ProcessAvatar memeriksa file gambar, memotongnya menjadi persegi di
// tengah, memperkecilnya menjadi AvatarSize x AvatarSize, lalu menyimpannya
// ulang sebagai JPEG.
//
// Menyimpan ulang (bukan menyimpan file asli) punya tiga manfaat:
//   - metadata EXIF ikut terbuang, termasuk lokasi GPS dari foto ponsel;
//   - file yang hanya berpura-pura menjadi gambar tidak akan lolos;
//   - semua foto profil berukuran kecil dan seragam (sekitar 10-30 KB).
//
// Padanannya di Node.js adalah library sharp, di Java Thumbnailator atau
// ImageIO.
func ProcessAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, &ValidationError{"image", "file kosong"}
	}
	if len(data) > MaxAvatarBytes {
		return nil, &ValidationError{"image", "maksimal 2 MiB"}
	}
	if ct := http.DetectContentType(data); !allowedAvatarTypes[ct] {
		return nil, &ValidationError{"image", "harus berupa gambar JPEG, PNG, atau WebP"}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, &ValidationError{"image", "gambar tidak bisa dibaca"}
	}
	if cfg.Width < minAvatarSide || cfg.Height < minAvatarSide {
		return nil, &ValidationError{"image", fmt.Sprintf("minimal %dx%d piksel", minAvatarSide, minAvatarSide)}
	}
	if cfg.Width*cfg.Height > maxAvatarPixels {
		return nil, &ValidationError{"image", "ukuran gambar terlalu besar (maksimal 25 megapiksel)"}
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &ValidationError{"image", "gambar tidak bisa dibaca"}
	}

	dst := image.NewRGBA(image.Rect(0, 0, AvatarSize, AvatarSize))
	// Latar putih: JPEG tidak punya transparansi, jadi bagian transparan
	// di PNG/WebP menjadi putih, bukan hitam.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	// CatmullRom: hasil perkecilan yang halus (lebih bagus dari
	// NearestNeighbor, sedikit lebih lambat). Untuk 256x256 tetap cepat.
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, centerSquare(src.Bounds()), draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: avatarQuality}); err != nil {
		return nil, fmt.Errorf("encode avatar: %w", err)
	}
	return out.Bytes(), nil
}

// centerSquare mengambil persegi terbesar di tengah gambar. Gambar
// 1200x800 menjadi area 800x800 mulai dari x=200.
func centerSquare(b image.Rectangle) image.Rectangle {
	side := min(b.Dx(), b.Dy())
	x := b.Min.X + (b.Dx()-side)/2
	y := b.Min.Y + (b.Dy()-side)/2
	return image.Rect(x, y, x+side, y+side)
}
