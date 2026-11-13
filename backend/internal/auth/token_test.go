package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "jwt-private.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600))

	got, err := LoadPrivateKey(path)
	require.NoError(t, err)
	assert.True(t, priv.Equal(got))

	_, err = LoadPrivateKey(filepath.Join(t.TempDir(), "tidak-ada.pem"))
	assert.Error(t, err)
}

func TestNewRefreshTokenIsRandomAndHashed(t *testing.T) {
	a, hashA, err := NewRefreshToken()
	require.NoError(t, err)
	b, _, err := NewRefreshToken()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.Len(t, hashA, 64) // SHA-256 dalam hex
	assert.Equal(t, hashA, HashRefreshToken(a))
}
