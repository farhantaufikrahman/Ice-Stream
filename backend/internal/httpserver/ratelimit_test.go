package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

func TestLimiterStoreRefillsAndReportsWait(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	s := newLimiterStore(rateRule{name: "t", every: 6 * time.Second, burst: 2}, func() time.Time { return now })

	ok, _ := s.allow("a")
	assert.True(t, ok)
	ok, _ = s.allow("a")
	assert.True(t, ok)
	ok, wait := s.allow("a")
	assert.False(t, ok)
	assert.Equal(t, 6*time.Second, wait)
	// Request yang ditolak tidak memakai token: setelah 6 detik boleh lagi.
	ok, wait = s.allow("a")
	assert.False(t, ok)
	assert.Equal(t, 6*time.Second, wait)

	// Kunci lain punya ember sendiri.
	ok, _ = s.allow("b")
	assert.True(t, ok)

	now = now.Add(6 * time.Second)
	ok, _ = s.allow("a")
	assert.True(t, ok)
}

func TestLimiterStoreSweepsIdleKeys(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	s := newLimiterStore(rateRule{name: "t", every: time.Second, burst: 5}, func() time.Time { return now })
	s.allow("lama")
	now = now.Add(2 * time.Minute)
	s.allow("baru")
	assert.Len(t, s.entries, 1)
	assert.Contains(t, s.entries, "baru")
}

func newRateLimitedRouter(t *testing.T, deps Deps, reg *prometheus.Registry) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Env:                "development",
		CORSAllowedOrigins: []string{"http://localhost:5173"},
		Cookie:             config.CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode},
		RateLimitEnabled:   true,
	}
	deps.Metrics = reg
	r, err := NewRouter(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), deps)
	require.NoError(t, err)
	return r
}

func TestLoginRateLimitedPerIP(t *testing.T) {
	reg := prometheus.NewRegistry()
	r := newRateLimitedRouter(t, Deps{Auth: &fakeAuthClient{}}, reg)

	login := func(ip string) *httptest.ResponseRecorder {
		return do(r, http.MethodPost, "/api/v1/auth/login", `{"login":"budi","password":"rahasia123"}`,
			func(req *http.Request) { req.RemoteAddr = ip + ":40000" })
	}
	for i := 0; i < ruleAuth.burst; i++ {
		require.NotEqual(t, http.StatusTooManyRequests, login("203.0.113.5").Code, "request ke-%d", i+1)
	}
	rec := login("203.0.113.5")
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "6", rec.Header().Get("Retry-After"))
	assert.Contains(t, rec.Body.String(), "RESOURCE_EXHAUSTED")

	// IP lain tidak ikut tertahan.
	assert.NotEqual(t, http.StatusTooManyRequests, login("203.0.113.6").Code)
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP icestream_http_rate_limited_total Jumlah request yang ditolak rate limit (429) per aturan.
# TYPE icestream_http_rate_limited_total counter
icestream_http_rate_limited_total{rule="auth"} 1
`), "icestream_http_rate_limited_total"))
}

func TestCommentRateLimitedPerUser(t *testing.T) {
	verifier, as := newTokenSigner(t)
	r := newRateLimitedRouter(t, Deps{Comments: &fakeCommentClient{}, Users: &fakeUserClient{}, Verifier: verifier}, prometheus.NewRegistry())

	for i := 0; i < ruleComment.burst; i++ {
		rec := do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"halo"}`, as(7))
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code, "request ke-%d", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"halo"}`, as(7)).Code)
	// User lain dari IP yang sama tetap boleh.
	assert.NotEqual(t, http.StatusTooManyRequests, do(r, http.MethodPost, "/api/v1/videos/42/comments", `{"body":"halo"}`, as(8)).Code)
}

func TestRateLimitDisabledByDefaultInTests(t *testing.T) {
	r := newTestRouter(t, Deps{Auth: &fakeAuthClient{}})
	for i := 0; i < ruleAuth.burst+5; i++ {
		rec := do(r, http.MethodPost, "/api/v1/auth/login", `{"login":"budi","password":"rahasia123"}`)
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code)
	}
}

func TestMetricsMiddlewareRecordsRoutes(t *testing.T) {
	reg := prometheus.NewRegistry()
	r := newRateLimitedRouter(t, Deps{}, reg)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	n, err := testutil.GatherAndCount(reg, "icestream_http_requests_total")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
