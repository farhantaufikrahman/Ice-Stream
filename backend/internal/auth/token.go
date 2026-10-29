package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Nilai iss dan aud di dalam JWT. Gateway menolak token yang nilainya beda.
const (
	TokenIssuer   = "ice-stream-auth"
	TokenAudience = "ice-stream-api"
)

// AccessClaims adalah isi access token.
type AccessClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// TokenMaker membuat access token (JWT) yang ditandatangani dengan Ed25519.
// Hanya AuthService yang memegang kunci privat; gateway cukup memegang kunci
// publik untuk memeriksa token, jadi gateway tidak bisa membuat token palsu.
type TokenMaker struct {
	key ed25519.PrivateKey
	ttl time.Duration
}

// NewTokenMaker membuat TokenMaker dari kunci privat dan umur token.
func NewTokenMaker(key ed25519.PrivateKey, ttl time.Duration) *TokenMaker {
	return &TokenMaker{key: key, ttl: ttl}
}

// LoadPrivateKey membaca kunci privat Ed25519 berformat PEM (PKCS#8),
// misalnya hasil `openssl genpkey -algorithm ed25519`.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("membaca kunci privat: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("kunci privat bukan format PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("membaca kunci privat: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("kunci privat bukan Ed25519")
	}
	return key, nil
}

// AccessToken membuat JWT untuk user dan waktu kedaluwarsanya.
func (m *TokenMaker) AccessToken(u User, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(m.ttl)
	claims := AccessClaims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   strconv.FormatInt(u.ID, 10),
			Audience:  jwt.ClaimStrings{TokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(m.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("menandatangani access token: %w", err)
	}
	return signed, expiresAt, nil
}

// NewRefreshToken membuat refresh token acak (32 byte) beserta hash-nya.
// Token asli dikirim ke user; yang disimpan di database hanya hash-nya.
func NewRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken menghitung SHA-256 dari refresh token, dalam bentuk hex.
// SHA-256 sudah cukup di sini (tidak perlu bcrypt) karena token-nya acak
// 256 bit, bukan password buatan manusia yang mudah ditebak.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
