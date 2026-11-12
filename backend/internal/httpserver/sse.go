package httpserver

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

// serveSSE membuka koneksi Server-Sent Events untuk satu room di hub
// (satu video untuk danmaku, satu siaran untuk chat live), lalu mengirim
// setiap pesan sebagai event bernama event sampai browser pergi atau pod
// berhenti. Dipakai bersama oleh danmaku (langkah 11) dan chat siaran
// langsung (langkah 25c).
//
// Padanannya: res.write("event: ...\ndata: ...\n\n") di Express, atau
// SseEmitter di Spring MVC.
// Dokumentasi format SSE: https://html.spec.whatwg.org/multipage/server-sent-events.html
func serveSSE(c *gin.Context, hub *live.Hub, roomID int64, event string, heartbeat time.Duration, logger *slog.Logger) {
	client, err := hub.Join(roomID)
	switch {
	case errors.Is(err, live.ErrFull):
		logger.WarnContext(c.Request.Context(), "penonton live penuh", slog.String("event", event))
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "terlalu banyak penonton, coba lagi sebentar")
		return
	case err != nil:
		logger.WarnContext(c.Request.Context(), "gagal bergabung ke siaran real-time", slog.String("event", event), slog.Any("error", err))
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "fitur real-time sedang tidak tersedia")
		return
	}
	defer hub.Leave(client)

	w := c.Writer
	rc := http.NewResponseController(w)
	// http.Server punya ReadTimeout 15 detik dan WriteTimeout 30 detik untuk
	// request biasa. Koneksi SSE berumur panjang, jadi batas itu dimatikan
	// untuk koneksi ini saja. Batas tulis diganti per pesan (lihat write).
	// Error diabaikan: ResponseRecorder di test tidak mendukung deadline.
	_ = rc.SetReadDeadline(time.Time{})

	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	// NGINX (NGINX Gateway Fabric) menampung response sebelum diteruskan
	// (buffering). Header ini meminta NGINX langsung meneruskan setiap event.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	write := func(chunk string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(liveWriteTimeout))
		if _, err := io.WriteString(w, chunk); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write(fmt.Sprintf("retry: %d\n\n", liveRetryMs)) {
		return
	}

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			// Browser menutup tab atau pindah halaman.
			return
		case <-client.Done():
			// Pod sedang berhenti. Browser akan menyambung ulang ke pod lain.
			return
		case msg := <-client.Messages():
			if !write("event: " + event + "\ndata: " + string(msg) + "\n\n") {
				return
			}
		case <-ticker.C:
			if !write(": ping\n\n") {
				return
			}
		}
	}
}
