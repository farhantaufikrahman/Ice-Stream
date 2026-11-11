package video

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Pencarian video memakai "inverted index" buatan sendiri, cara kerja dasar
// semua mesin pencari (Elasticsearch, Meilisearch, Oracle Text):
//
//	judul "Belajar Golang dari Nol"  ->  belajar, golang, dari, nol
//
// Setiap kata disimpan di tabel video_search_terms bersama id videonya.
// Saat user mencari "golang", Oracle cukup membaca index untuk kata yang
// diawali "golang", tanpa membaca semua judul video satu per satu.

const (
	// Kata lebih pendek dari ini tidak disimpan dan tidak dicari: "a" atau
	// "d" cocok dengan terlalu banyak video untuk berguna.
	minTermRunes = 2
	// Kata lebih panjang dipotong. Sama dengan panjang kolom term (30 CHAR).
	maxTermRunes = 30
	// Batas kata per video. Deskripsi 1000 huruf berisi sekitar 150 kata.
	maxTermsPerVideo = 300
	// Batas kata kunci dalam satu pencarian.
	maxQueryTerms  = 5
	maxQueryRunes  = 100
	searchMaxPages = 10
)

// Bobot kata. Kata di judul lebih penting daripada kata di deskripsi.
const (
	WeightDescription = 1
	WeightTitle       = 2
)

// SearchTerm adalah satu kata di inverted index.
type SearchTerm struct {
	Term string
	// Weight bernilai WeightTitle jika kata ada di judul, selain itu
	// WeightDescription.
	Weight int
}

// BuildSearchTerms memecah judul, deskripsi, dan tag menjadi daftar kata
// unik. Kata yang ada di judul dan deskripsi hanya muncul sekali, dengan
// bobot judul. Tag berbobot sama dengan judul: kreator memilihnya sendiri
// sebagai kata kunci video (tag "esKrim" menjadi kata "eskrim").
func BuildSearchTerms(title, description string, tags []string) []SearchTerm {
	weights := make(map[string]int)
	var order []string
	add := func(text string, weight int) {
		for _, t := range tokenize(text) {
			if len(order) >= maxTermsPerVideo {
				return
			}
			old, seen := weights[t]
			if !seen {
				order = append(order, t)
			}
			if weight > old {
				weights[t] = weight
			}
		}
	}
	add(title, WeightTitle)
	// Sebelum deskripsi: kalau deskripsi sangat panjang dan batas
	// maxTermsPerVideo tercapai, tag tidak ikut terpotong.
	for _, t := range tags {
		add(t, WeightTitle)
	}
	add(description, WeightDescription)

	terms := make([]SearchTerm, len(order))
	for i, t := range order {
		terms[i] = SearchTerm{Term: t, Weight: weights[t]}
	}
	return terms
}

// ParseSearchQuery mengubah kata kunci dari user menjadi daftar kata unik
// (maksimal 5) dengan aturan yang sama seperti BuildSearchTerms, supaya
// "Café" di kotak pencarian cocok dengan "cafe" di judul.
func ParseSearchQuery(q string) ([]string, error) {
	if !utf8.ValidString(q) {
		return nil, &ValidationError{"query", "bukan teks UTF-8 yang valid"}
	}
	if utf8.RuneCountInString(q) > maxQueryRunes {
		return nil, &ValidationError{"query", "maksimal 100 karakter"}
	}
	seen := make(map[string]bool)
	var terms []string
	for _, t := range tokenize(q) {
		if seen[t] {
			continue
		}
		seen[t] = true
		terms = append(terms, t)
		if len(terms) == maxQueryTerms {
			break
		}
	}
	if len(terms) == 0 {
		return nil, &ValidationError{"query", "isi minimal satu kata dengan 2 huruf atau lebih"}
	}
	return terms, nil
}

// tokenize menormalkan teks lalu memecahnya menjadi kata:
//  1. huruf kecil semua;
//  2. tanda aksen dibuang ("é" menjadi "e"): NFD memisahkan huruf dari
//     tandanya, lalu tandanya (kategori Mn) dilewati;
//  3. semua yang bukan huruf atau angka menjadi pemisah kata.
//
// Huruf Mandarin, Jepang, dan Korea tidak memakai spasi, jadi satu kalimat
// menjadi satu "kata" panjang. Untuk itu dibutuhkan pemecah kata khusus
// (misalnya bigram); belum dibuat di langkah ini.
func tokenize(text string) []string {
	var (
		out []string
		cur strings.Builder
		n   int
	)
	flush := func() {
		if n >= minTermRunes {
			out = append(out, cur.String())
		}
		cur.Reset()
		n = 0
	}
	for _, r := range norm.NFD.String(strings.ToLower(text)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// Tanda aksen: lewati, kata belum selesai.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if n < maxTermRunes {
				cur.WriteRune(r)
				n++
			}
		default:
			flush()
		}
	}
	flush()
	return out
}

// SearchVideos mencari video READY yang judul atau deskripsinya berisi
// kata berawalan setiap kata di query.
//
// Halaman memakai offset (lewati N hasil pertama), bukan cursor seperti
// feed, karena urutannya berdasarkan skor: tidak ada satu kolom urut yang
// bisa dipakai sebagai cursor. Offset besar makin berat untuk database,
// jadi pencarian dibatasi 10 halaman, seperti mesin pencari pada umumnya.
func (s *Service) SearchVideos(ctx context.Context, query string, pageSize int, pageToken string) ([]Video, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	page := 0
	if pageToken != "" {
		p, err := decodeSearchPage(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		page = p
	}
	terms, err := ParseSearchQuery(query)
	if err != nil {
		return nil, "", err
	}

	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	videos, err := s.repo.Search(ctx, terms, page*pageSize, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	if len(videos) <= pageSize || page+1 >= searchMaxPages {
		if len(videos) > pageSize {
			videos = videos[:pageSize]
		}
		return videos, "", nil
	}
	return videos[:pageSize], encodeSearchPage(page + 1), nil
}

// IndexPending menyimpan kata-kata semua video yang belum diindeks, 100
// video per putaran, sampai habis. Dipanggil sekali saat VideoService start
// (di goroutine), untuk video yang dibuat sebelum fitur pencarian ada.
// Video baru diindeks langsung saat dibuat (CreateUpload), jadi tidak lewat
// sini. Mengembalikan jumlah video yang diindeks.
func (s *Service) IndexPending(ctx context.Context) (int, error) {
	total := 0
	for {
		videos, err := s.repo.ListUnindexed(ctx, 100)
		if err != nil {
			return total, err
		}
		if len(videos) == 0 {
			return total, nil
		}
		indexed := 0
		for _, v := range videos {
			ok, err := s.repo.SaveSearchTerms(ctx, v.ID, BuildSearchTerms(v.Title, v.Description, v.Tags), s.now())
			if err != nil {
				return total, err
			}
			if ok {
				indexed++
			}
		}
		total += indexed
		// Pengaman: kalau satu putaran tidak mengindeks apa pun, berhenti
		// supaya tidak berputar selamanya.
		if indexed == 0 {
			return total, nil
		}
	}
}

// Token halaman berisi nomor halaman, misalnya base64("s2") untuk halaman
// ketiga. Huruf "s" membedakannya dari token feed.
func encodeSearchPage(page int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("s" + strconv.Itoa(page)))
}

func decodeSearchPage(token string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	numStr, ok := strings.CutPrefix(string(raw), "s")
	if !ok {
		return 0, errors.New("bukan token pencarian")
	}
	page, err := strconv.Atoi(numStr)
	if err != nil || page < 1 || page >= searchMaxPages {
		return 0, errors.New("nomor halaman salah")
	}
	return page, nil
}
