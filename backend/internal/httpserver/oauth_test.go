package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/autherr"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

func (f *fakeAuthClient) LoginWithIdentity(_ context.Context, in *authv1.LoginWithIdentityRequest, _ ...grpc.CallOption) (*authv1.LoginWithIdentityResponse, error) {
	f.lastIdentity = in
	if f.identityErr != nil {
		return nil, f.identityErr
	}
	return &authv1.LoginWithIdentityResponse{LoginCode: "kode-sekali"}, nil
}

func (f *fakeAuthClient) ExchangeLoginCode(_ context.Context, in *authv1.ExchangeLoginCodeRequest, _ ...grpc.CallOption) (*authv1.ExchangeLoginCodeResponse, error) {
	f.lastExchange = in
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	return &authv1.ExchangeLoginCodeResponse{User: testUser(), Tokens: testTokens("3")}, nil
}

// fakeGitHub meniru endpoint token dan API GitHub yang dipakai oauth.go.
func fakeGitHub(t *testing.T, primaryVerified bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.Form.Get("code") != "code-bagus" || r.Form.Get("code_verifier") == "" {
			http.Error(w, `{"error":"bad_verification_code"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok-gh","token_type":"bearer"}`)
	})
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok-gh" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /user", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":583231,"login":"octocat","name":"The Octocat","email":null}`)
	}))
	mux.HandleFunc("GET /user/emails", auth(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "lain@example.com", "primary": false, "verified": true},
			{"email": "octo@example.com", "primary": true, "verified": primaryVerified},
		})
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newOAuthTestRouter: GitHub menyala dan diarahkan ke server palsu.
func newOAuthTestRouter(t *testing.T, client *fakeAuthClient, gh *httptest.Server) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OAuthConfig{
		RedirectBaseURL: "http://localhost:4000",
		AppBaseURL:      "http://ice-stream.localhost:4000",
		GitHub:          config.OAuthProvider{ClientID: "gh-id", ClientSecret: "gh-secret"},
	}
	refresh := &authHandler{client: client, cookie: config.CookieConfig{SameSite: http.SameSiteLaxMode}, logger: logger}
	h := newOAuthHandler(cfg, client, refresh, logger)
	if gh != nil {
		h.providers["github"].config.Endpoint = oauth2.Endpoint{AuthURL: gh.URL + "/authorize", TokenURL: gh.URL + "/token"}
		old := githubAPIURL
		githubAPIURL = gh.URL
		t.Cleanup(func() { githubAPIURL = old })
	}
	r := gin.New()
	g := r.Group("/api/v1/auth")
	g.GET("/oauth/providers", h.listProviders)
	g.GET("/oauth/:provider/start", h.start)
	g.GET("/oauth/:provider/callback", h.callback)
	g.POST("/oauth/exchange", h.exchange)
	return r
}

// startLogin menjalankan /start dan mengembalikan cookie state dan
// parameter state dari URL halaman izin.
func startLogin(t *testing.T, r http.Handler, next string) (*http.Cookie, string) {
	t.Helper()
	rec := do(r, http.MethodGet, "/api/v1/auth/oauth/github/start?next="+url.QueryEscape(next), "")
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	q := loc.Query()
	assert.Equal(t, "gh-id", q.Get("client_id"))
	assert.Equal(t, "http://localhost:4000/api/v1/auth/oauth/github/callback", q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookieName {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	return cookie, q.Get("state")
}

// finishFragment membaca nilai di fragment redirect ke /oauth/selesai.
func finishFragment(t *testing.T, rec *httptest.ResponseRecorder) url.Values {
	t.Helper()
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	loc := rec.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "http://ice-stream.localhost:4000/oauth/selesai#"), loc)
	u, err := url.Parse(loc)
	require.NoError(t, err)
	v, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	return v
}

func TestOAuthProviders(t *testing.T) {
	r := newOAuthTestRouter(t, &fakeAuthClient{}, nil)
	rec := do(r, http.MethodGet, "/api/v1/auth/oauth/providers", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"providers":[{"id":"github","start_url":"http://localhost:4000/api/v1/auth/oauth/github/start"}]}`, rec.Body.String())

	// Google tidak dinyalakan.
	rec = do(r, http.MethodGet, "/api/v1/auth/oauth/google/start", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOAuthGitHubLogin(t *testing.T) {
	client := &fakeAuthClient{}
	r := newOAuthTestRouter(t, client, fakeGitHub(t, true))
	cookie, state := startLogin(t, r, "/studio")

	rec := do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?code=code-bagus&state="+state, "", func(req *http.Request) { req.AddCookie(cookie) })
	frag := finishFragment(t, rec)
	assert.Equal(t, "kode-sekali", frag.Get("code"))
	assert.Equal(t, "/studio", frag.Get("next"))
	assert.Empty(t, frag.Get("error"))

	require.NotNil(t, client.lastIdentity)
	assert.Equal(t, "github", client.lastIdentity.GetProvider())
	assert.Equal(t, "583231", client.lastIdentity.GetSubject())
	assert.Equal(t, "octo@example.com", client.lastIdentity.GetEmail(), "email utama, bukan yang lain")
	assert.True(t, client.lastIdentity.GetEmailVerified())
	assert.Equal(t, "octocat", client.lastIdentity.GetUsernameHint())

	// Kode ditukar di alamat aplikasi: cookie refresh token dipasang.
	rec = do(r, http.MethodPost, "/api/v1/auth/oauth/exchange", `{"code":"kode-sekali"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "refresh-3", refreshCookie(t, rec).Value)
	assert.Contains(t, rec.Body.String(), `"access_token":"access-3"`)
	assert.Equal(t, "kode-sekali", client.lastExchange.GetLoginCode())
}

func TestOAuthCallbackRejectsBadState(t *testing.T) {
	client := &fakeAuthClient{}
	r := newOAuthTestRouter(t, client, fakeGitHub(t, true))
	cookie, state := startLogin(t, r, "")

	// State tidak cocok (CSRF), atau tanpa cookie.
	rec := do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?code=code-bagus&state=palsu", "", func(req *http.Request) { req.AddCookie(cookie) })
	assert.Equal(t, "state", finishFragment(t, rec).Get("error"))
	rec = do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?code=code-bagus&state="+state, "")
	assert.Equal(t, "state", finishFragment(t, rec).Get("error"))
	assert.Nil(t, client.lastIdentity)

	// User menekan Batal di halaman izin.
	rec = do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?error=access_denied&state="+state, "", func(req *http.Request) { req.AddCookie(cookie) })
	assert.Equal(t, "denied", finishFragment(t, rec).Get("error"))

	// Code ditolak penyedia.
	rec = do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?code=salah&state="+state, "", func(req *http.Request) { req.AddCookie(cookie) })
	assert.Equal(t, "failed", finishFragment(t, rec).Get("error"))
}

func TestOAuthIdentityErrors(t *testing.T) {
	reason := func(r string) error {
		st, err := status.New(codes.FailedPrecondition, "x").WithDetails(&errdetails.ErrorInfo{Domain: autherr.Domain, Reason: r})
		require.NoError(t, err)
		return st.Err()
	}
	for want, err := range map[string]error{
		"email_unverified":     reason(autherr.ReasonEmailNotVerified),
		"email_needs_password": reason(autherr.ReasonEmailNeedsPassword),
		"already_linked":       reason(autherr.ReasonProviderAlreadyLinked),
		"banned":               status.Error(codes.PermissionDenied, "diblokir"),
		"failed":               status.Error(codes.Internal, "x"),
	} {
		client := &fakeAuthClient{identityErr: err}
		r := newOAuthTestRouter(t, client, fakeGitHub(t, false))
		cookie, state := startLogin(t, r, "")
		rec := do(r, http.MethodGet, "/api/v1/auth/oauth/github/callback?code=code-bagus&state="+state, "", func(req *http.Request) { req.AddCookie(cookie) })
		frag := finishFragment(t, rec)
		assert.Equal(t, want, frag.Get("error"))
		assert.Empty(t, frag.Get("code"))
		assert.False(t, client.lastIdentity.GetEmailVerified(), "email utama belum diverifikasi diteruskan apa adanya")
	}
}

func TestOAuthExchangeInvalidCode(t *testing.T) {
	r := newOAuthTestRouter(t, &fakeAuthClient{exchangeErr: status.Error(codes.Unauthenticated, "kode login tidak valid")}, nil)
	rec := do(r, http.MethodPost, "/api/v1/auth/oauth/exchange", `{"code":"basi"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/studio":            "/studio",
		"/watch/42?t=10":     "/watch/42?t=10",
		"":                   "",
		"//jahat.com":        "",
		"/\\jahat.com":       "",
		"https://jahat.com":  "",
		"javascript:alert()": "",
	} {
		assert.Equal(t, want, safeNext(in), in)
	}
}

// TestOAuthEndpointsMatchLibrary memastikan alamat yang ditulis sendiri
// sama dengan daftar resmi di golang.org/x/oauth2/endpoints.
func TestOAuthEndpointsMatchLibrary(t *testing.T) {
	assert.Equal(t, endpoints.Google.AuthURL, googleAuthURL)
	assert.Equal(t, endpoints.Google.TokenURL, googleTokenURL)
	assert.Equal(t, endpoints.GitHub.AuthURL, githubAuthURL)
	assert.Equal(t, endpoints.GitHub.TokenURL, githubTokenURL)
}
