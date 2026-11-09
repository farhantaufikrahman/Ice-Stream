package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

const (
	// Refresh token disimpan di cookie httpOnly, jadi JavaScript di browser
	// tidak bisa membacanya (aman dari pencurian lewat XSS). Path dibatasi
	// ke /api/v1/auth supaya cookie tidak ikut terkirim ke endpoint lain.
	refreshCookieName = "ice_refresh"
	refreshCookiePath = "/api/v1/auth"

	// Batas waktu satu panggilan ke AuthService. bcrypt butuh sekitar 0,3 detik.
	authCallTimeout = 5 * time.Second
	// Body JSON auth hanya beberapa ratus byte.
	maxAuthBodyBytes = 16 << 10 // 16 KiB
)

// authHandler meneruskan request REST dari browser ke AuthService (gRPC).
// Gateway tidak menyimpan data apa pun; semua aturan ada di AuthService.
type authHandler struct {
	client authv1.AuthServiceClient
	cookie config.CookieConfig
	logger *slog.Logger
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	// Username atau email.
	Login    string `json:"login"`
	Password string `json:"password"`
}

type userResponse struct {
	// ID dikirim sebagai string, karena angka int64 bisa melebihi batas
	// angka aman di JavaScript (2^53).
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type accessTokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type loginResponse struct {
	User userResponse `json:"user"`
	accessTokenResponse
}

func (h *authHandler) register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()

	resp, err := h.client.Register(ctx, &authv1.RegisterRequest{
		Username:    req.Username,
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": toUserResponse(resp.GetUser())})
}

func (h *authHandler) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()

	resp, err := h.client.Login(ctx, &authv1.LoginRequest{
		Login:    req.Login,
		Password: req.Password,
		Client:   clientInfo(c),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	tokens := resp.GetTokens()
	h.setRefreshCookie(c, tokens)
	c.JSON(http.StatusOK, loginResponse{
		User:                toUserResponse(resp.GetUser()),
		accessTokenResponse: toAccessTokenResponse(tokens),
	})
}

func (h *authHandler) refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err != nil || refreshToken == "" {
		abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "refresh token tidak ada, silakan login")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()

	resp, err := h.client.RefreshToken(ctx, &authv1.RefreshTokenRequest{
		RefreshToken: refreshToken,
		Client:       clientInfo(c),
	})
	if err != nil {
		// Token ditolak (dicabut/kedaluwarsa): hapus cookie-nya juga, supaya
		// browser tidak terus mengirim token yang sudah mati.
		if status.Code(err) == codes.Unauthenticated {
			h.clearRefreshCookie(c)
		}
		writeGRPCError(c, h.logger, err)
		return
	}
	tokens := resp.GetTokens()
	h.setRefreshCookie(c, tokens)
	c.JSON(http.StatusOK, toAccessTokenResponse(tokens))
}

func (h *authHandler) logout(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err == nil && refreshToken != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
		defer cancel()
		if _, err := h.client.Logout(ctx, &authv1.LogoutRequest{RefreshToken: refreshToken}); err != nil {
			// Cookie sengaja tidak dihapus, supaya user bisa mencoba logout
			// lagi dan token di database benar-benar dicabut.
			writeGRPCError(c, h.logger, err)
			return
		}
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

// me menjawab siapa pemilik access token. Berguna untuk frontend mengecek
// apakah user masih login. Data profil lengkap menyusul di langkah berikutnya.
func (h *authHandler) me(c *gin.Context) {
	claims, ok := CurrentUser(c)
	if !ok {
		abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak ada")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": claims.Subject, "role": claims.Role})
}

func (h *authHandler) setRefreshCookie(c *gin.Context, tokens *authv1.TokenPair) {
	expiresAt := tokens.GetRefreshTokenExpiresAt().AsTime()
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    tokens.GetRefreshToken(),
		Path:     refreshCookiePath,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: h.cookie.SameSite,
	})
}

func (h *authHandler) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1, // hapus sekarang
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: h.cookie.SameSite,
	})
}

// bindJSON membaca body JSON. Jika gagal, langsung menjawab 400 atau 413
// dan mengembalikan false.
func bindJSON(c *gin.Context, dst any) bool {
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		abortError(c, http.StatusRequestEntityTooLarge, "TOO_LARGE", "body request terlalu besar")
		return false
	}
	abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "body harus berupa JSON yang valid")
	return false
}

// clientInfo mengambil User-Agent dan IP browser untuk dicatat AuthService.
func clientInfo(c *gin.Context) *authv1.ClientInfo {
	return &authv1.ClientInfo{UserAgent: c.Request.UserAgent(), IpAddress: c.ClientIP()}
}

func toUserResponse(u *authv1.User) userResponse {
	return userResponse{
		ID:          strconv.FormatInt(u.GetId(), 10),
		Username:    u.GetUsername(),
		Email:       u.GetEmail(),
		DisplayName: u.GetDisplayName(),
		Role:        u.GetRole(),
		CreatedAt:   u.GetCreatedAt().AsTime(),
	}
}

func toAccessTokenResponse(t *authv1.TokenPair) accessTokenResponse {
	return accessTokenResponse{
		AccessToken: t.GetAccessToken(),
		TokenType:   "Bearer",
		ExpiresAt:   t.GetAccessTokenExpiresAt().AsTime(),
	}
}
