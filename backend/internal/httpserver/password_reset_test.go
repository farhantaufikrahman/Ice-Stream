package httpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
)

func (f *fakeAuthClient) RequestPasswordReset(_ context.Context, in *authv1.RequestPasswordResetRequest, _ ...grpc.CallOption) (*authv1.RequestPasswordResetResponse, error) {
	f.lastForgot = in
	if f.forgotErr != nil {
		return nil, f.forgotErr
	}
	return &authv1.RequestPasswordResetResponse{}, nil
}

func (f *fakeAuthClient) ResetPassword(_ context.Context, in *authv1.ResetPasswordRequest, _ ...grpc.CallOption) (*authv1.ResetPasswordResponse, error) {
	f.lastReset = in
	if f.resetErr != nil {
		return nil, f.resetErr
	}
	return &authv1.ResetPasswordResponse{}, nil
}

func TestForgotPassword(t *testing.T) {
	fake := &fakeAuthClient{}
	r := newTestRouter(t, Deps{Auth: fake})

	rec := do(r, http.MethodPost, "/api/v1/auth/password/forgot", `{"email":"budi@example.com"}`, func(req *http.Request) {
		req.RemoteAddr = "192.0.2.10:5555"
	})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "Jika email itu terdaftar")
	assert.Equal(t, "budi@example.com", fake.lastForgot.GetEmail())
	assert.Equal(t, "192.0.2.10", fake.lastForgot.GetClient().GetIpAddress())

	fake.forgotErr = status.Error(codes.InvalidArgument, "email: format email tidak valid")
	rec = do(r, http.MethodPost, "/api/v1/auth/password/forgot", `{"email":"bukan-email"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(r, http.MethodPost, "/api/v1/auth/password/forgot", `{"email":`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "JSON rusak")
}

func TestResetPassword(t *testing.T) {
	fake := &fakeAuthClient{}
	r := newTestRouter(t, Deps{Auth: fake})

	rec := do(r, http.MethodPost, "/api/v1/auth/password/reset", `{"token":"abc","new_password":"passwordBaru9"}`, withRefreshCookie("rt-lama"))
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "abc", fake.lastReset.GetToken())
	assert.Equal(t, "passwordBaru9", fake.lastReset.GetNewPassword())
	assert.Less(t, refreshCookie(t, rec).MaxAge, 0, "cookie refresh lama dihapus")

	fake.resetErr = status.Error(codes.FailedPrecondition, "tautan reset tidak valid atau sudah kedaluwarsa, minta tautan baru")
	rec = do(r, http.MethodPost, "/api/v1/auth/password/reset", `{"token":"abc","new_password":"passwordBaru9"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "FAILED_PRECONDITION")
	assert.Empty(t, rec.Result().Cookies(), "gagal: cookie tidak disentuh")
}
