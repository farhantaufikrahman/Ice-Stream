// Package accesstoken berisi format access token (JWT) Ice-Stream dan cara
// memeriksanya dengan kunci publik Ed25519.
//
// Package ini dipakai bersama oleh AuthService (yang membuat token) dan API
// gateway (yang memeriksa token). Sengaja dibuat kecil dan tidak mengimpor
// package auth, supaya gateway tidak ikut membawa GORM/godror (CGO).
package accesstoken

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Nilai iss dan aud di dalam JWT. Token yang nilainya beda ditolak.
const (
	Issuer   = "ice-stream-auth"
	Audience = "ice-stream-api"
)

// ErrInvalid dikembalikan Verify untuk semua token yang tidak sah:
// rusak, tanda tangan salah, kedaluwarsa, atau iss/aud tidak cocok.
var ErrInvalid = errors.New("access token tidak valid")

// Claims adalah isi access token.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// UserID mengembalikan id user dari klaim sub.
func (c Claims) UserID() (int64, error) {
	return strconv.ParseInt(c.Subject, 10, 64)
}

// Verifier memeriksa access token memakai kunci publik.
type Verifier struct {
	key    ed25519.PublicKey
	parser *jwt.Parser
}

// NewVerifier membuat Verifier dari kunci publik Ed25519.
func NewVerifier(key ed25519.PublicKey) *Verifier {
	return &Verifier{
		key: key,
		parser: jwt.NewParser(
			// Hanya terima EdDSA. Tanpa ini, penyerang bisa mengirim token
			// dengan "alg":"none" atau algoritma lain.
			jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
			jwt.WithIssuer(Issuer),
			jwt.WithAudience(Audience),
			jwt.WithExpirationRequired(),
			// Toleransi selisih jam antar server.
			jwt.WithLeeway(30*time.Second),
		),
	}
}

// Verify memeriksa tanda tangan dan isi token, lalu mengembalikan klaimnya.
func (v *Verifier) Verify(token string) (Claims, error) {
	var claims Claims
	_, err := v.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return v.key, nil })
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := claims.UserID(); err != nil {
		return Claims{}, fmt.Errorf("%w: sub bukan angka", ErrInvalid)
	}
	return claims, nil
}

// LoadPublicKey membaca kunci publik Ed25519 berformat PEM (SPKI),
// misalnya hasil `openssl pkey -in jwt-private.pem -pubout`.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("membaca kunci publik: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("kunci publik bukan format PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("membaca kunci publik: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("kunci publik bukan Ed25519")
	}
	return key, nil
}
