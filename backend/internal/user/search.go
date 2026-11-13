package user

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pencarian kreator (langkah 22c). Berbeda dengan pencarian video yang
// memakai inverted index (VideoService, video_search_terms), jumlah user
// jauh lebih kecil dan yang dicari hanya username dan nama tampilan, jadi
// cukup LIKE '%kata%' pada dua kolom itu.

const (
	// Aturan kata kunci sama dengan pencarian video, supaya kotak pencarian
	// berperilaku sama di semua tab.
	minSearchTermRunes = 2
	maxSearchTerms     = 5
	maxSearchRunes     = 100
	defaultSearchPage  = 20
	maxSearchPage      = 50
	// Halaman memakai offset; offset besar makin berat, jadi dibatasi
	// seperti pencarian video.
	searchMaxPages = 10
	// searchCountLimit: angka di tab pencarian berhenti di 1001, yang
	// ditampilkan "1.000+" (sama dengan pencarian video).
	searchCountLimit = 1001
)

// parseSearchTerms memecah kata kunci menjadi kata unik huruf kecil.
// Semua yang bukan huruf atau angka menjadi pemisah, jadi kata-katanya
// tidak pernah berisi % atau _ (karakter khusus LIKE) dan aman dipakai
// di pola LIKE tanpa escape.
func parseSearchTerms(q string) ([]string, error) {
	if !utf8.ValidString(q) {
		return nil, &ValidationError{"query", "bukan teks UTF-8 yang valid"}
	}
	if utf8.RuneCountInString(q) > maxSearchRunes {
		return nil, &ValidationError{"query", "maksimal 100 karakter"}
	}
	words := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool)
	var terms []string
	for _, w := range words {
		if utf8.RuneCountInString(w) < minSearchTermRunes || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
		if len(terms) == maxSearchTerms {
			break
		}
	}
	if len(terms) == 0 {
		return nil, &ValidationError{"query", "isi minimal satu kata dengan 2 huruf atau lebih"}
	}
	return terms, nil
}

// SearchUsers mencari kreator. Email tidak pernah disertakan.
func (s *Service) SearchUsers(ctx context.Context, query string, pageSize int, pageToken string) ([]Profile, string, error) {
	if pageSize == 0 {
		pageSize = defaultSearchPage
	}
	if pageSize < 0 || pageSize > maxSearchPage {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("harus antara 1 dan %d", maxSearchPage)}
	}
	page := 0
	if pageToken != "" {
		p, err := decodeSearchPage(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		page = p
	}
	terms, err := parseSearchTerms(query)
	if err != nil {
		return nil, "", err
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	found, err := s.repo.SearchUsers(ctx, terms, page*pageSize, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(found) > pageSize {
		found = found[:pageSize]
		if page+1 < searchMaxPages {
			next = encodeSearchPage(page + 1)
		}
	}
	for i := range found {
		found[i].Email = ""
	}
	return found, next, nil
}

// CountSearchUsers menghitung semua hasil SearchUsers (langkah 27).
// firstPage adalah jumlah kreator di halaman pertama; hasMore false
// berarti halaman pertama sudah memuat semua hasil, jadi Oracle tidak
// perlu ditanya lagi.
func (s *Service) CountSearchUsers(ctx context.Context, query string, firstPage int, hasMore bool) (int64, error) {
	if !hasMore {
		return int64(firstPage), nil
	}
	terms, err := parseSearchTerms(query)
	if err != nil {
		return 0, err
	}
	return s.repo.CountSearchUsers(ctx, terms, searchCountLimit)
}

// encodeSearchPage: base64 dari "s<nomor halaman>", sama dengan token
// pencarian video.
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
