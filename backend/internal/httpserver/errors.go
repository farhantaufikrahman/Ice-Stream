package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Semua error dari gateway berbentuk:
//
//	{"error": {"code": "ALREADY_EXISTS", "message": "username sudah dipakai"}}
//
// code tetap (untuk dibaca kode frontend), message boleh ditampilkan ke user.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func abortError(c *gin.Context, httpStatus int, code, message string) {
	c.AbortWithStatusJSON(httpStatus, gin.H{"error": errorBody{Code: code, Message: message}})
}

// writeGRPCError menerjemahkan error dari service gRPC (AuthService,
// UserService, VideoService, CommentService) menjadi respons HTTP. Pesan
// untuk error 4xx berasal dari service dan memang ditujukan untuk user.
// Error lain diganti pesan umum, detailnya hanya di log.
func writeGRPCError(c *gin.Context, logger *slog.Logger, err error) {
	st := status.Convert(err)
	switch st.Code() {
	case codes.InvalidArgument:
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", st.Message())
	case codes.AlreadyExists:
		abortError(c, http.StatusConflict, "ALREADY_EXISTS", st.Message())
	case codes.Unauthenticated:
		abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", st.Message())
	case codes.PermissionDenied:
		abortError(c, http.StatusForbidden, "PERMISSION_DENIED", st.Message())
	case codes.NotFound:
		abortError(c, http.StatusNotFound, "NOT_FOUND", st.Message())
	case codes.FailedPrecondition:
		// Permintaannya benar, tapi keadaan data belum memungkinkan
		// (misalnya file video belum selesai diunggah).
		abortError(c, http.StatusConflict, "FAILED_PRECONDITION", st.Message())
	case codes.Unavailable:
		logger.WarnContext(c.Request.Context(), "service gRPC tidak tersedia", slog.Any("error", err))
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "layanan sedang tidak tersedia, coba lagi sebentar")
	case codes.DeadlineExceeded:
		logger.WarnContext(c.Request.Context(), "service gRPC terlalu lama menjawab", slog.Any("error", err))
		abortError(c, http.StatusGatewayTimeout, "TIMEOUT", "layanan terlalu lama menjawab, coba lagi")
	case codes.Canceled:
		// Browser sudah menutup koneksi; tidak ada yang perlu dijawab.
		if errors.Is(c.Request.Context().Err(), context.Canceled) {
			c.Abort()
			return
		}
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "request dibatalkan")
	default:
		logger.ErrorContext(c.Request.Context(), "error dari service gRPC", slog.Any("error", err))
		abortError(c, http.StatusInternalServerError, "INTERNAL", "terjadi kesalahan internal")
	}
}
