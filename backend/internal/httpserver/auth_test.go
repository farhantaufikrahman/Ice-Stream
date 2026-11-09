package httpserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
)

// fakeAuthClient menggantikan AuthService sungguhan di test. Setiap field
// *Err diisi untuk mensimulasikan error dari AuthService.
type fakeAuthClient struct {
	registerErr, loginErr, refreshErr, logoutErr error

	lastLogin   *authv1.LoginRequest
	lastRefresh *authv1.RefreshTokenRequest
	lastLogout  *authv1.LogoutRequest
}

var _ authv1.AuthServiceClient = (*fakeAuthClient)(nil)

var testCreatedAt = time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC)

func testUser() *authv1.User {
	return &authv1.User{Id: 7, Username: "kyrie", Email: "kyrie@example.com", DisplayName: "Kyrie", Role: "USER", CreatedAt: timestamppb.New(testCreatedAt)}
}

func testTokens(n string) *authv1.TokenPair {
	now := time.Now()
	return &authv1.TokenPair{
		AccessToken:           "access-" + n,
		AccessTokenExpiresAt:  timestamppb.New(now.Add(15 * time.Minute)),
		RefreshToken:          "refresh-" + n,
		RefreshTokenExpiresAt: timestamppb.New(now.Add(720 * time.Hour)),
	}
}

func (f *fakeAuthClient) Register(_ context.Context, _ *authv1.RegisterRequest, _ ...grpc.CallOption) (*authv1.RegisterResponse, error) {
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return &authv1.RegisterResponse{User: testUser()}, nil
}

func (f *fakeAuthClient) Login(_ context.Context, in *authv1.LoginRequest, _ ...grpc.CallOption) (*authv1.LoginResponse, error) {
	f.lastLogin = in
	if f.loginErr != nil {
		return nil, f.loginErr
	}
	return &authv1.LoginResponse{User: testUser(), Tokens: testTokens("1")}, nil
}

func (f *fakeAuthClient) RefreshToken(_ context.Context, in *authv1.RefreshTokenRequest, _ ...grpc.CallOption) (*authv1.RefreshTokenResponse, error) {
	f.lastRefresh = in
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return &authv1.RefreshTokenResponse{Tokens: testTokens("2")}, nil
}

func (f *fakeAuthClient) Logout(_ context.Context, in *authv1.LogoutRequest, _ ...grpc.CallOption) (*authv1.LogoutResponse, error) {
	f.lastLogout = in
	if f.logoutErr != nil {
		return nil, f.logoutErr
	}
	return &authv1.LogoutResponse{}, nil
}

func do(r http.Handler, method, path, body string, modify ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, m := range modify {
		m(req)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func withRefreshCookie(value string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: value}) }
}

func refreshCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == refreshCookieName {
			return c
		}
	}
	t.Fatalf("cookie %s tidak ada di respons", refreshCookieName)
	return nil
}

func TestRegister(t *testing.T) {
	r := newTestRouter(t, Deps{Auth: &fakeAuthClient{}})

	rec := do(r, http.MethodPost, "/api/v1/auth/register", `{"username":"kyrie","email":"kyrie@example.com","password":"rahasia123"}`)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"user":{"id":"7","username":"kyrie","email":"kyrie@example.com","display_name":"Kyrie","role":"USER","created_at":"2026-10-29T04:00:00Z"}}`, rec.Body.String())
}

func TestLoginSetsRefreshCookie(t *testing.T) {
	fake := &fakeAuthClient{}
	r := newTestRouter(t, Deps{Auth: fake})

	rec := do(r, http.MethodPost, "/api/v1/auth/login", `{"login":"kyrie","password":"rahasia123"}`, func(req *http.Request) {
		req.Header.Set("User-Agent", "test-browser")
		req.Header.Set("X-Forwarded-For", "6.6.6.6") // harus diabaikan
		req.RemoteAddr = "192.0.2.10:5555"
	})

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "access-1", body["access_token"])
	assert.Equal(t, "Bearer", body["token_type"])
	assert.NotContains(t, rec.Body.String(), "refresh-1", "refresh token tidak boleh ada di body")

	c := refreshCookie(t, rec)
	assert.Equal(t, "refresh-1", c.Value)
	assert.Equal(t, "/api/v1/auth", c.Path)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Greater(t, c.MaxAge, 0)

	assert.Equal(t, "test-browser", fake.lastLogin.GetClient().GetUserAgent())
	assert.Equal(t, "192.0.2.10", fake.lastLogin.GetClient().GetIpAddress())
}

func TestRefresh(t *testing.T) {
	t.Run("tanpa cookie", func(t *testing.T) {
		r := newTestRouter(t, Deps{Auth: &fakeAuthClient{}})
		rec := do(r, http.MethodPost, "/api/v1/auth/refresh", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("berhasil merotasi cookie", func(t *testing.T) {
		fake := &fakeAuthClient{}
		r := newTestRouter(t, Deps{Auth: fake})
		rec := do(r, http.MethodPost, "/api/v1/auth/refresh", "", withRefreshCookie("rt-1"))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "rt-1", fake.lastRefresh.GetRefreshToken())
		assert.Equal(t, "refresh-2", refreshCookie(t, rec).Value)
		assert.Contains(t, rec.Body.String(), `"access_token":"access-2"`)
	})

	t.Run("token ditolak menghapus cookie", func(t *testing.T) {
		fake := &fakeAuthClient{refreshErr: status.Error(codes.Unauthenticated, "refresh token tidak valid")}
		r := newTestRouter(t, Deps{Auth: fake})
		rec := do(r, http.MethodPost, "/api/v1/auth/refresh", "", withRefreshCookie("rt-lama"))

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Less(t, refreshCookie(t, rec).MaxAge, 0)
	})
}

func TestLogout(t *testing.T) {
	t.Run("mencabut token dan menghapus cookie", func(t *testing.T) {
		fake := &fakeAuthClient{}
		r := newTestRouter(t, Deps{Auth: fake})
		rec := do(r, http.MethodPost, "/api/v1/auth/logout", "", withRefreshCookie("rt-1"))

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, "rt-1", fake.lastLogout.GetRefreshToken())
		assert.Less(t, refreshCookie(t, rec).MaxAge, 0)
	})

	t.Run("tanpa cookie tetap sukses", func(t *testing.T) {
		fake := &fakeAuthClient{}
		r := newTestRouter(t, Deps{Auth: fake})
		rec := do(r, http.MethodPost, "/api/v1/auth/logout", "")

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Nil(t, fake.lastLogout)
	})

	t.Run("auth mati: cookie tidak dihapus", func(t *testing.T) {
		fake := &fakeAuthClient{logoutErr: status.Error(codes.Unavailable, "connection refused")}
		r := newTestRouter(t, Deps{Auth: fake})
		rec := do(r, http.MethodPost, "/api/v1/auth/logout", "", withRefreshCookie("rt-1"))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Empty(t, rec.Result().Cookies())
	})
}

func TestMe(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, accesstoken.Claims{
		Role: "ADMIN",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: accesstoken.Issuer, Subject: "7", Audience: jwt.ClaimStrings{accesstoken.Audience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}).SignedString(priv)
	require.NoError(t, err)

	r := newTestRouter(t, Deps{Auth: &fakeAuthClient{}, Verifier: accesstoken.NewVerifier(pub)})
	bearer := func(v string) func(*http.Request) {
		return func(req *http.Request) { req.Header.Set("Authorization", v) }
	}

	rec := do(r, http.MethodGet, "/api/v1/auth/me", "", bearer("Bearer "+token))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"user_id":"7","role":"ADMIN"}`, rec.Body.String())

	for name, header := range map[string]string{
		"tanpa header":     "",
		"bukan Bearer":     "Basic abc",
		"token rusak":      "Bearer abc.def.ghi",
		"Bearer kosong":    "Bearer ",
		"token diubah":     "Bearer " + token + "x",
		"skema huruf aneh": "bEaReR " + token + "x",
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(r, http.MethodGet, "/api/v1/auth/me", "", bearer(header))
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
		})
	}
}

func TestAuthBadBody(t *testing.T) {
	r := newTestRouter(t, Deps{Auth: &fakeAuthClient{}})

	rec := do(r, http.MethodPost, "/api/v1/auth/login", `{"login":`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	big := `{"login":"` + strings.Repeat("a", maxAuthBodyBytes) + `"}`
	rec = do(r, http.MethodPost, "/api/v1/auth/login", big)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestGRPCErrorMapping(t *testing.T) {
	tests := []struct {
		code       codes.Code
		wantStatus int
		wantCode   string
	}{
		{codes.InvalidArgument, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{codes.AlreadyExists, http.StatusConflict, "ALREADY_EXISTS"},
		{codes.Unauthenticated, http.StatusUnauthorized, "UNAUTHENTICATED"},
		{codes.PermissionDenied, http.StatusForbidden, "PERMISSION_DENIED"},
		{codes.Unavailable, http.StatusServiceUnavailable, "UNAVAILABLE"},
		{codes.DeadlineExceeded, http.StatusGatewayTimeout, "TIMEOUT"},
		{codes.Internal, http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			fake := &fakeAuthClient{registerErr: status.Error(tt.code, "pesan dari auth")}
			r := newTestRouter(t, Deps{Auth: fake})
			rec := do(r, http.MethodPost, "/api/v1/auth/register", `{}`)

			assert.Equal(t, tt.wantStatus, rec.Code)
			var body struct {
				Error errorBody `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantCode, body.Error.Code)
			if tt.wantStatus >= 500 {
				assert.NotContains(t, body.Error.Message, "pesan dari auth", "detail error internal tidak boleh bocor")
			}
		})
	}
}
