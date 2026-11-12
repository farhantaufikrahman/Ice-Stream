// Package textrule berisi aturan teks pendek yang dipakai lebih dari satu
// service, supaya danmaku video dan chat siaran langsung selalu memakai
// aturan yang sama.
package textrule

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SingleLine membuang spasi di awal dan akhir, lalu memeriksa teks satu
// baris: 1 sampai maxRunes huruf, tanpa baris baru, tab, atau karakter
// kontrol tak terlihat. Error-nya berisi pesan untuk user (bahasa
// Indonesia), tanpa nama field.
func SingleLine(body string, maxRunes int) (string, error) {
	if !utf8.ValidString(body) {
		return "", errors.New("bukan teks UTF-8 yang valid")
	}
	body = strings.TrimSpace(body)
	n := utf8.RuneCountInString(body)
	if n == 0 || n > maxRunes {
		return "", fmt.Errorf("wajib diisi, maksimal %d karakter", maxRunes)
	}
	for _, r := range body {
		if r == '\n' || r == '\r' || r == '\t' {
			return "", errors.New("harus satu baris")
		}
		// Cc = karakter kontrol, Cf = karakter format tak terlihat (misalnya
		// U+202E pembalik arah teks). U+200D (zero width joiner) tetap boleh
		// karena dipakai untuk menyusun emoji.
		if unicode.Is(unicode.Cc, r) || (unicode.Is(unicode.Cf, r) && r != '‍') {
			return "", errors.New("berisi karakter yang tidak diizinkan")
		}
	}
	return body, nil
}
