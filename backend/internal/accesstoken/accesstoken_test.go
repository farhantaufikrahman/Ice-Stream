package accesstoken

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub, priv
}

func sign(t *testing.T, key any, method jwt.SigningMethod, c Claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	require.NoError(t, err)
	return s
}

func validClaims() Claims {
	now := time.Now()
	return Claims{
		Role: "USER",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   "42",
			Audience:  jwt.ClaimStrings{Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
	}
}

func TestVerifyValidToken(t *testing.T) {
	pub, priv := newKey(t)

	claims, err := NewVerifier(pub).Verify(sign(t, priv, jwt.SigningMethodEdDSA, validClaims()))
	require.NoError(t, err)

	id, err := claims.UserID()
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
	assert.Equal(t, "USER", claims.Role)
}

func TestVerifyRejects(t *testing.T) {
	pub, priv := newKey(t)
	_, otherPriv := newKey(t)

	expired := validClaims()
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	wrongAud := validClaims()
	wrongAud.Audience = jwt.ClaimStrings{"aplikasi-lain"}
	wrongIss := validClaims()
	wrongIss.Issuer = "penipu"
	noExp := validClaims()
	noExp.ExpiresAt = nil
	badSub := validClaims()
	badSub.Subject = "bukan-angka"

	tests := map[string]string{
		"kunci lain":        sign(t, otherPriv, jwt.SigningMethodEdDSA, validClaims()),
		"kedaluwarsa":       sign(t, priv, jwt.SigningMethodEdDSA, expired),
		"aud salah":         sign(t, priv, jwt.SigningMethodEdDSA, wrongAud),
		"iss salah":         sign(t, priv, jwt.SigningMethodEdDSA, wrongIss),
		"tanpa exp":         sign(t, priv, jwt.SigningMethodEdDSA, noExp),
		"sub bukan angka":   sign(t, priv, jwt.SigningMethodEdDSA, badSub),
		"alg none":          sign(t, jwt.UnsafeAllowNoneSignatureType, jwt.SigningMethodNone, validClaims()),
		"bukan jwt":         "abc.def.ghi",
		"string kosong":     "",
		"hmac dengan kunci": sign(t, []byte(pub), jwt.SigningMethodHS256, validClaims()),
	}
	v := NewVerifier(pub)
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(token)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestLoadPublicKey(t *testing.T) {
	pub, _ := newKey(t)
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "jwt-public.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600))

	got, err := LoadPublicKey(path)
	require.NoError(t, err)
	assert.True(t, pub.Equal(got))

	_, err = LoadPublicKey(filepath.Join(t.TempDir(), "tidak-ada.pem"))
	assert.Error(t, err)
}
