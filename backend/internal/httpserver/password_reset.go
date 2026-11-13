package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
)

// Langkah 24c: "Lupa kata sandi?".
//
// Kedua endpoint ini tanpa login. Gateway hanya meneruskan ke AuthService;
// pembuatan token, batas permintaan, dan pengiriman email ada di sana.

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// forgotPassword: POST /api/v1/auth/password/forgot {"email": "..."}
//
// Selalu 202 Accepted (selama format email benar), baik email terdaftar
// atau tidak. 202 artinya "permintaan diterima dan diproses nanti", cocok
// karena email dikirim di background.
func (h *authHandler) forgotPassword(c *gin.Context) {
	var req forgotPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()

	_, err := h.client.RequestPasswordReset(ctx, &authv1.RequestPasswordResetRequest{
		Email:  req.Email,
		Client: clientInfo(c),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "Jika email itu terdaftar, tautan untuk mengatur ulang kata sandi sudah dikirim.",
	})
}

// resetPassword: POST /api/v1/auth/password/reset {"token": "...", "new_password": "..."}
//
// 204 jika berhasil. 409 FAILED_PRECONDITION jika tautan tidak valid,
// kedaluwarsa, atau sudah dipakai; 400 jika password baru tidak memenuhi
// aturan.
func (h *authHandler) resetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authCallTimeout)
	defer cancel()

	_, err := h.client.ResetPassword(ctx, &authv1.ResetPasswordRequest{
		Token:       req.Token,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	// Semua refresh token user sudah dicabut AuthService. Jika browser ini
	// masih menyimpan cookie lama, cookie itu dihapus juga.
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}
