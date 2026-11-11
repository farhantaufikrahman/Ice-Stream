package video

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Kategori dan tag video (langkah 23).
//
// Kategori dipilih dari daftar tetap, seperti YouTube ("Gaming", "Music")
// dan Bilibili (分区). Daftar tetap membuat chip di beranda dan filter feed
// bisa memakai index. Tag bebas diisi kreator, seperti hashtag.

// Categories adalah daftar kategori yang boleh dipakai, sesuai urutan chip
// di beranda (desain Home). Nilainya sama dengan CHECK constraint
// videos_category_ck (migrasi V15). Label tampilannya ("Anime", "Musik")
// ada di frontend.
var Categories = []string{"anime", "game", "musik", "teknologi", "kuliner", "vlog", "edukasi", "olahraga"}

const (
	// maxTags: YouTube membatasi tag dengan total 500 karakter, Bilibili
	// dengan 10 tag. Ice-Stream memakai 10 tag, masing-masing 2-30 huruf.
	maxTags     = 10
	minTagRunes = 2
	maxTagRunes = 30
)

// Details adalah data video yang bisa diubah pemiliknya.
type Details struct {
	Title       string
	Description string
	// Category kosong = tanpa kategori.
	Category string
	Tags     []string
}

// FeedFilter membatasi isi feed publik (ListPublic). Field kosong = tidak
// dibatasi.
type FeedFilter struct {
	// OwnerIDs: hanya video milik salah satu user ini.
	OwnerIDs []int64
	// Category: hanya video dengan kategori ini.
	Category string
}

// cleanCategory memeriksa kategori. Huruf besar diterima ("Game" menjadi
// "game"). Kosong berarti tanpa kategori.
func cleanCategory(raw string) (string, error) {
	c := strings.ToLower(strings.TrimSpace(raw))
	if c == "" || isCategory(c) {
		return c, nil
	}
	return "", &ValidationError{"category", "harus salah satu dari: " + strings.Join(Categories, ", ")}
}

func isCategory(c string) bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

// cleanTags merapikan daftar tag dari form:
//   - "#" di depan dibuang, jadi "#esKrim" dan "esKrim" sama saja;
//   - tag kosong dilewati;
//   - tag yang sama (tanpa melihat huruf besar/kecil) hanya disimpan
//     sekali, dengan tulisan yang pertama kali diketik;
//   - setiap tag hanya boleh berisi huruf dan angka, 2-30 karakter;
//   - maksimal 10 tag.
//
// Hanya huruf dan angka, karena tag ditampilkan sebagai "#esKrim" dan
// dicari lewat kata pencarian; tanda baca dan spasi akan memecahnya.
func cleanTags(raw []string) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		// NFC: "é" yang diketik sebagai "e" + tanda aksen menjadi satu
		// huruf, jadi dua tulisan yang terlihat sama dianggap sama.
		t = norm.NFC.String(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" {
			continue
		}
		if !validTag(t) {
			return nil, &ValidationError{"tags", fmt.Sprintf("tag %q harus %d-%d huruf atau angka, tanpa spasi dan tanda baca", t, minTagRunes, maxTagRunes)}
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, &ValidationError{"tags", fmt.Sprintf("maksimal %d tag", maxTags)}
	}
	return out, nil
}

func validTag(t string) bool {
	if !utf8.ValidString(t) {
		return false
	}
	n := 0
	for _, r := range t {
		// Mn = tanda aksen yang tidak bisa digabung oleh NFC; tetap
		// bagian dari huruf sebelumnya.
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Mn, r) {
			return false
		}
		n++
	}
	return n >= minTagRunes && n <= maxTagRunes
}

// joinTags dan splitTags mengubah tag ke dan dari kolom videos.tags
// ("esKrim anakKos"). Aman karena tag tidak pernah berisi spasi.
func joinTags(tags []string) string { return strings.Join(tags, " ") }

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
