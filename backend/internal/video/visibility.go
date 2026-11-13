package video

import (
	"strings"
)

// Visibilitas dan draf video (langkah 26, migrasi V20). Desain Upload:
// "Publik", "Tidak publik", "Pribadi", tombol "Simpan draf" dan "Terbitkan".
//
// VisibilityPublic dan VisibilityPrivate sudah ada sejak playlist
// (playlist.go) dan artinya sama untuk video:
//   - PUBLIC: tampil di feed, pencarian, Trending, dan kanal;
//   - PRIVATE: "Pribadi", hanya pemiliknya.
//
// VisibilityBlocked (langkah 28) ada di moderation.go.
//
// VisibilityUnlisted hanya untuk video: "Tidak publik", bisa dibuka siapa
// saja yang punya tautannya, tapi tidak tampil di daftar mana pun.
const VisibilityUnlisted = "UNLISTED"

// Languages adalah pilihan "Bahasa video" di desain Upload (kode ISO
// 639-1), sama dengan CHECK constraint videos_language_ck.
var Languages = []string{"id", "en", "ja"}

// Syarat SQL yang dipakai berulang kali. Teksnya tetap (bukan dari input
// user), jadi aman disambung ke query.
const (
	// listedSQL: video tampil di daftar publik (feed, pencarian, Trending,
	// kanal, notifikasi video baru).
	listedSQL = "status = 'READY' AND draft = 0 AND visibility = 'PUBLIC'"
	// listedSQLv: sama dengan listedSQL untuk query yang memberi tabel
	// videos alias v.
	listedSQLv = "v.status = 'READY' AND v.draft = 0 AND v.visibility = 'PUBLIC'"
	// watchableSQL: video boleh ditonton siapa saja yang tahu tautannya
	// (PUBLIC atau UNLISTED). Dipakai untuk like, views, playlist, dan
	// daftar milik user lain.
	watchableSQL  = "status = 'READY' AND draft = 0 AND visibility IN ('PUBLIC', 'UNLISTED')"
	watchableSQLv = "v.status = 'READY' AND v.draft = 0 AND v.visibility IN ('PUBLIC', 'UNLISTED')"
)

// IsListed: video tampil di daftar publik. Sama dengan listedSQL.
func (v Video) IsListed() bool {
	return v.Status == StatusReady && !v.Draft && v.Visibility == VisibilityPublic
}

// IsWatchable: video boleh ditonton orang lain lewat tautan. Sama dengan
// watchableSQL.
func (v Video) IsWatchable() bool {
	return v.Status == StatusReady && !v.Draft && (v.Visibility == VisibilityPublic || v.Visibility == VisibilityUnlisted)
}

// CanView: viewerID (0 = belum login) boleh membuka video READY ini.
// Pemilik selalu boleh.
func (v Video) CanView(viewerID int64) bool {
	if viewerID != 0 && viewerID == v.OwnerID {
		return v.Status == StatusReady
	}
	return v.IsWatchable()
}

// cleanVideoVisibility: kosong = PUBLIC (bawaan, sama dengan sebelum
// langkah 26).
func cleanVideoVisibility(raw string) (string, error) {
	switch s := strings.ToUpper(strings.TrimSpace(raw)); s {
	case "":
		return VisibilityPublic, nil
	case VisibilityPublic, VisibilityUnlisted, VisibilityPrivate:
		return s, nil
	}
	return "", &ValidationError{"visibility", "harus PUBLIC, UNLISTED, atau PRIVATE"}
}

// cleanLanguage: kosong = tidak diisi.
func cleanLanguage(raw string) (string, error) {
	l := strings.ToLower(strings.TrimSpace(raw))
	if l == "" {
		return "", nil
	}
	for _, known := range Languages {
		if l == known {
			return l, nil
		}
	}
	return "", &ValidationError{"language", "harus salah satu dari: " + strings.Join(Languages, ", ")}
}
