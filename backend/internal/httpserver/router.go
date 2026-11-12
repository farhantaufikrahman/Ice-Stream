// Package httpserver menyusun router Gin untuk API gateway Ice-Stream.
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

// ReadinessCheck memeriksa satu dependensi (Oracle, NATS, dan seterusnya).
// Kembalikan error jika dependensi belum siap menerima traffic.
type ReadinessCheck func(ctx context.Context) error

// Deps berisi semua yang dibutuhkan router dari luar. Dikumpulkan dalam satu
// struct supaya mudah diganti dengan versi palsu di test.
type Deps struct {
	// Checks berisi pemeriksaan untuk /readyz; boleh kosong.
	Checks map[string]ReadinessCheck
	// Auth adalah klien gRPC ke AuthService.
	Auth authv1.AuthServiceClient
	// Videos adalah klien gRPC ke VideoService.
	Videos videov1.VideoServiceClient
	// Users adalah klien gRPC ke UserService (berjalan di proses AuthService).
	Users userv1.UserServiceClient
	// Comments adalah klien gRPC ke CommentService (berjalan di proses
	// VideoService).
	Comments commentv1.CommentServiceClient
	// Danmaku adalah klien gRPC ke DanmakuService (berjalan di proses
	// VideoService).
	Danmaku danmakuv1.DanmakuServiceClient
	// LiveStreams adalah klien gRPC ke LiveService (siaran langsung,
	// berjalan di proses VideoService).
	LiveStreams livev1.LiveServiceClient
	// Live membagikan danmaku baru ke penonton yang tersambung (SSE).
	// Boleh nil di test.
	Live *live.Hub
	// LiveChat membagikan chat siaran langsung baru ke penonton (SSE,
	// langkah 25c). Boleh nil di test.
	LiveChat *live.Hub
	// Verifier memeriksa access token di endpoint yang butuh login.
	Verifier *accesstoken.Verifier
}

// NewRouter membuat router dengan middleware dan semua route.
func NewRouter(cfg config.Config, logger *slog.Logger, deps Deps) (*gin.Engine, error) {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// Hanya percaya header X-Forwarded-For dari proxy yang terdaftar.
	// Tanpa ini, siapa pun bisa memalsukan IP-nya dengan header tersebut.
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("TRUSTED_PROXIES tidak valid: %w", err)
	}
	r.Use(gin.Recovery(), requestLogger(logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORSAllowedOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // dibutuhkan untuk cookie refresh token
		MaxAge:           12 * time.Hour,
	}))

	// Probe Kubernetes ada di luar /api/v1, sesuai rancangan.
	r.GET("/healthz", healthz)
	r.GET("/readyz", readyz(deps.Checks))

	v1 := r.Group("/api/v1")
	v1.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	auth := &authHandler{client: deps.Auth, cookie: cfg.Cookie, logger: logger}
	authGroup := v1.Group("/auth", limitBody(maxAuthBodyBytes))
	authGroup.POST("/register", auth.register)
	authGroup.POST("/login", auth.login)
	authGroup.POST("/refresh", auth.refresh)
	authGroup.POST("/logout", auth.logout)
	authGroup.POST("/password/forgot", auth.forgotPassword)
	authGroup.POST("/password/reset", auth.resetPassword)
	authGroup.GET("/me", RequireAuth(deps.Verifier), auth.me)
	// Masuk dengan Google / GitHub (langkah 26, oauth.go).
	oauth := newOAuthHandler(cfg.OAuth, deps.Auth, auth, logger)
	authGroup.GET("/oauth/providers", oauth.listProviders)
	authGroup.GET("/oauth/:provider/start", oauth.start)
	authGroup.GET("/oauth/:provider/callback", oauth.callback)
	authGroup.POST("/oauth/exchange", oauth.exchange)

	videos := &videoHandler{client: deps.Videos, users: deps.Users, logger: logger}
	requireAuth := RequireAuth(deps.Verifier)
	videoGroup := v1.Group("/videos", limitBody(maxVideoBodyBytes))
	videoGroup.GET("", videos.list)
	videoGroup.POST("", requireAuth, videos.create)
	videoGroup.POST("/:id/complete", requireAuth, videos.complete)
	videoGroup.GET("/:id", OptionalAuth(deps.Verifier), videos.get)
	videoGroup.PATCH("/:id", requireAuth, videos.update)
	videoGroup.DELETE("/:id", requireAuth, videos.remove)
	videoGroup.POST("/:id/views", OptionalAuth(deps.Verifier), videos.recordView)
	videoGroup.PUT("/:id/like", requireAuth, videos.like)
	videoGroup.DELETE("/:id/like", requireAuth, videos.unlike)
	videoGroup.POST("/:id/reports", requireAuth, videos.report)
	// Thumbnail (langkah 26). Gambar bisa sampai 2 MiB, jadi tidak
	// memakai batas 16 KiB videoGroup; batasnya dicek di handler.
	videoGroup.PUT("/:id/thumbnail/frame", requireAuth, videos.chooseThumbnailFrame)
	v1.PUT("/videos/:id/thumbnail", requireAuth, videos.uploadThumbnail)
	v1.GET("/me/videos", requireAuth, videos.listMine)
	v1.GET("/search/videos", videos.search)
	v1.GET("/trending/videos", videos.trending)
	v1.GET("/me/history", requireAuth, videos.history)
	v1.DELETE("/me/history", requireAuth, videos.clearHistory)
	v1.PUT("/me/history/:id", requireAuth, limitBody(maxHistoryBodyBytes), videos.saveProgress)
	v1.DELETE("/me/history/:id", requireAuth, videos.deleteHistory)
	v1.GET("/me/watch-later", requireAuth, videos.watchLater)
	v1.PUT("/me/watch-later/:id", requireAuth, videos.addWatchLater)
	v1.DELETE("/me/watch-later/:id", requireAuth, videos.removeWatchLater)

	// Playlist (langkah 21c). Daftar playlist satu kanal ada di bawah
	// /users/:username/playlists (lihat bagian users).
	playlistGroup := v1.Group("/playlists", limitBody(maxPlaylistBodyBytes))
	playlistGroup.POST("", requireAuth, videos.createPlaylist)
	playlistGroup.GET("/:id", OptionalAuth(deps.Verifier), videos.getPlaylist)
	playlistGroup.PATCH("/:id", requireAuth, videos.updatePlaylist)
	playlistGroup.DELETE("/:id", requireAuth, videos.deletePlaylist)
	playlistGroup.GET("/:id/videos", OptionalAuth(deps.Verifier), videos.playlistVideos)
	playlistGroup.PUT("/:id/videos/:videoId", requireAuth, videos.addPlaylistVideo)
	playlistGroup.DELETE("/:id/videos/:videoId", requireAuth, videos.removePlaylistVideo)
	v1.GET("/me/playlists", requireAuth, videos.myPlaylists)
	// Dasbor Creator Studio (langkah 22b).
	v1.GET("/me/channel-stats", requireAuth, videos.channelStats)

	comments := &commentHandler{client: deps.Comments, users: deps.Users, logger: logger}
	videoGroup.GET("/:id/comments", comments.list)
	videoGroup.POST("/:id/comments", requireAuth, comments.create)
	videoGroup.GET("/:id/comments/:commentId/replies", comments.listReplies)
	videoGroup.DELETE("/:id/comments/:commentId", requireAuth, comments.remove)
	// Halaman Komentar di Creator Studio (langkah 26).
	v1.GET("/me/comments", requireAuth, comments.listChannel)

	danmaku := &danmakuHandler{client: deps.Danmaku, videos: deps.Videos, hub: deps.Live, heartbeat: liveHeartbeat, logger: logger}
	videoGroup.GET("/:id/danmaku", danmaku.list)
	videoGroup.POST("/:id/danmaku", requireAuth, danmaku.send)
	videoGroup.GET("/:id/danmaku/live", danmaku.live)
	// Halaman Danmaku di Creator Studio (langkah 26).
	studioDanmaku := &studioDanmakuHandler{danmakuHandler: danmaku, users: deps.Users}
	v1.GET("/me/danmaku", requireAuth, studioDanmaku.listChannel)
	v1.DELETE("/me/danmaku/:id", requireAuth, studioDanmaku.remove)

	// Siaran langsung (langkah 25a-25c).
	lives := &liveHandler{client: deps.LiveStreams, users: deps.Users, chatHub: deps.LiveChat, heartbeat: liveHeartbeat, logger: logger}
	v1.GET("/live", lives.list)
	liveGroup := v1.Group("/live/:id", limitBody(maxLiveBodyBytes))
	liveGroup.GET("", lives.get)
	liveGroup.GET("/chat", lives.listChat)
	liveGroup.POST("/chat", requireAuth, lives.sendChat)
	liveGroup.GET("/chat/live", lives.chatLive)
	meLive := v1.Group("/me/live", requireAuth, limitBody(maxLiveBodyBytes))
	meLive.GET("", lives.myChannel)
	meLive.PATCH("", lives.updateMyChannel)
	meLive.POST("/stream-key", lives.resetStreamKey)
	meLive.POST("/publish-token", lives.createPublishToken)

	notifications := &notificationHandler{videos: deps.Videos, users: deps.Users, logger: logger}
	v1.GET("/me/notifications", requireAuth, notifications.list)
	v1.GET("/me/notifications/unread-count", requireAuth, notifications.unreadCount)
	v1.POST("/me/notifications/read-all", requireAuth, notifications.markAllRead)
	v1.POST("/me/notifications/:id/read", requireAuth, notifications.markRead)

	users := &userHandler{users: deps.Users, videos: videos, logger: logger}
	v1.GET("/users/:username", OptionalAuth(deps.Verifier), users.getByUsername)
	// Tab Kreator dan Playlist di halaman pencarian (langkah 22c).
	v1.GET("/search/channels", OptionalAuth(deps.Verifier), users.searchChannels)
	v1.GET("/search/playlists", videos.searchPlaylists)
	v1.PUT("/users/:username/subscription", requireAuth, users.subscribe)
	v1.DELETE("/users/:username/subscription", requireAuth, users.unsubscribe)
	v1.GET("/me/subscriptions", requireAuth, users.subscriptions)
	v1.GET("/me/feed", requireAuth, users.feed)
	v1.GET("/users/:username/videos", users.listVideos)
	v1.GET("/users/:username/playlists", OptionalAuth(deps.Verifier), users.listPlaylists)
	v1.GET("/me/profile", requireAuth, users.me)
	v1.PATCH("/me/profile", requireAuth, limitBody(maxProfileBodyBytes), users.update)
	// Batas 2 MiB dicek di handler, supaya pesan errornya jelas.
	v1.PUT("/me/avatar", requireAuth, users.uploadAvatar)
	v1.DELETE("/me/avatar", requireAuth, users.removeAvatar)

	return r, nil
}

// healthz (liveness) hanya menjawab "proses ini hidup".
// Jangan cek database di sini: kalau Oracle mati, Kubernetes akan
// me-restart pod terus-menerus padahal pod-nya sendiri sehat.
func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz (readiness) menjawab "siap menerima traffic". Jika satu dependensi
// gagal, Kubernetes berhenti mengirim request ke pod ini sampai pulih.
func readyz(checks map[string]ReadinessCheck) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		results := make(map[string]string, len(checks))
		status := http.StatusOK
		for name, check := range checks {
			if err := check(ctx); err != nil {
				results[name] = err.Error()
				status = http.StatusServiceUnavailable
				continue
			}
			results[name] = "ok"
		}

		c.JSON(status, gin.H{"status": http.StatusText(status), "checks": results})
	}
}

// requestLogger mencatat setiap request dalam format log terstruktur.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}
