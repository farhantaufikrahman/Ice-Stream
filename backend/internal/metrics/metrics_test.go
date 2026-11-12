package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHTTPMiddlewareUsesRoutePattern(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	m := NewHTTP(reg)
	r := gin.New()
	r.Use(m.Middleware())
	r.GET("/videos/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, path := range []string{"/videos/1", "/videos/2", "/tidak-ada"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	assert.Equal(t, 2.0, testutil.ToFloat64(m.requests.WithLabelValues("GET", "/videos/:id", "200")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.requests.WithLabelValues("GET", "unmatched", "404")))

	rec := httptest.NewRecorder()
	Handler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `icestream_http_requests_total{method="GET",route="/videos/:id",status="200"} 2`)
	assert.Contains(t, body, "go_goroutines")
	assert.False(t, strings.Contains(body, "/videos/1"), "URL asli tidak boleh menjadi label")
}

func TestGRPCUnary(t *testing.T) {
	m := NewGRPC(NewRegistry())
	info := &grpc.UnaryServerInfo{FullMethod: "/video.v1.VideoService/GetVideo"}
	ok := func(context.Context, any) (any, error) { return "ok", nil }
	notFound := func(context.Context, any) (any, error) { return nil, status.Error(codes.NotFound, "tidak ada") }

	_, _ = m.Unary()(context.Background(), nil, info, ok)
	_, err := m.Unary()(context.Background(), nil, info, notFound)
	assert.Equal(t, codes.NotFound, status.Code(err))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.handled.WithLabelValues(info.FullMethod, "OK")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.handled.WithLabelValues(info.FullMethod, "NotFound")))
}
