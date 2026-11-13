package httpserver

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
)

// claimsKey adalah kunci di gin.Context tempat RequireAuth menyimpan klaim.
const claimsKey = "accesstoken.claims"

// RequireAuth menolak request tanpa access token yang sah (401). Jika token
// sah, klaimnya disimpan supaya handler bisa membacanya dengan CurrentUser.
// Padanannya: passport-jwt di Express, atau filter JWT di Spring Security.
func RequireAuth(v *accesstoken.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			c.Header("WWW-Authenticate", "Bearer")
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak ada")
			return
		}
		claims, err := v.Verify(raw)
		if err != nil {
			// Alasan detail (kedaluwarsa, tanda tangan salah, ...) sengaja
			// tidak dikirim, supaya tidak membantu orang yang memalsukan token.
			c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak valid atau kedaluwarsa")
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}

// OptionalAuth dipakai di endpoint yang boleh dibuka tanpa login, tapi
// hasilnya bisa berbeda untuk user yang login (misalnya pemilik video
// melihat videonya yang belum selesai diunggah). Tanpa header Authorization,
// request diteruskan sebagai pengunjung. Dengan header tapi tokennya tidak
// sah, request ditolak 401, supaya frontend tahu harus refresh token.
func OptionalAuth(v *accesstoken.Verifier) gin.HandlerFunc {
	required := RequireAuth(v)
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.Next()
			return
		}
		required(c)
	}
}

// CurrentUser mengembalikan klaim yang disimpan RequireAuth.
func CurrentUser(c *gin.Context) (accesstoken.Claims, bool) {
	v, ok := c.Get(claimsKey)
	if !ok {
		return accesstoken.Claims{}, false
	}
	claims, ok := v.(accesstoken.Claims)
	return claims, ok
}

// bearerToken mengambil <token> dari header "Authorization: Bearer <token>".
func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// limitBody membatasi ukuran body request. Body auth hanya beberapa ratus
// byte, jadi batas kecil mencegah orang mengirim body raksasa.
func limitBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
